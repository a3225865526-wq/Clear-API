package model

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var ErrTokenStorage = errors.New("API credential storage unavailable")

func TokenEncryptionEnabled() bool { return os.Getenv("CLEARAPI_TOKEN_ENCRYPTION") == "true" }

var tokenCipherCache struct {
	sync.Mutex
	path string
	aead cipher.AEAD
}

func tokenCipher() (cipher.AEAD, error) {
	path := os.Getenv("CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE")
	if path == "" {
		return nil, ErrTokenStorage
	}
	tokenCipherCache.Lock()
	defer tokenCipherCache.Unlock()
	if tokenCipherCache.path == path && tokenCipherCache.aead != nil {
		return tokenCipherCache.aead, nil
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 128 {
		return nil, ErrTokenStorage
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != 32 {
		return nil, ErrTokenStorage
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrTokenStorage
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrTokenStorage
	}
	tokenCipherCache.path, tokenCipherCache.aead = path, aead
	return aead, nil
}

func tokenDigest(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

func encryptTokenKey(key string) (string, error) {
	if key == "" || len(key) > 64 {
		return "", ErrTokenStorage
	}
	aead, err := tokenCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrTokenStorage
	}
	sealed := aead.Seal(nonce, nonce, []byte(key), []byte("clearapi-api-token-v1"))
	return "enc1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func decryptTokenKey(value string) (string, error) {
	if value == "" {
		return "", nil
	} // a SELECT projection without key
	if !strings.HasPrefix(value, "enc1:") {
		if TokenEncryptionEnabled() {
			return "", ErrTokenStorage
		}
		return value, nil
	}
	aead, err := tokenCipher()
	if err != nil {
		return "", err
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, "enc1:"))
	if err != nil || len(data) < aead.NonceSize()+aead.Overhead() {
		return "", ErrTokenStorage
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("clearapi-api-token-v1"))
	if err != nil {
		return "", ErrTokenStorage
	}
	return string(plain), nil
}

type tokenKeySerializer struct{}

func init() { schema.RegisterSerializer("clearapi_token", tokenKeySerializer{}) }

func (tokenKeySerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	var value string
	switch v := dbValue.(type) {
	case string:
		value = v
	case []byte:
		value = string(v)
	case nil:
		return field.Set(ctx, dst, "")
	default:
		return ErrTokenStorage
	}
	plain, err := decryptTokenKey(value)
	if err != nil {
		return err
	}
	return field.Set(ctx, dst, plain)
}

func (tokenKeySerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, fieldValue any) (any, error) {
	key, ok := fieldValue.(string)
	if !ok {
		return nil, ErrTokenStorage
	}
	if !TokenEncryptionEnabled() {
		return key, nil
	}
	return encryptTokenKey(key)
}

func (token *Token) BeforeCreate(tx *gorm.DB) error {
	if TokenEncryptionEnabled() {
		digest := tokenDigest(token.Key)
		token.KeyDigest = &digest
	}
	return nil
}

// Explicit, offline migration. It bypasses credential serialization so both
// legacy rows and encrypted rows can be verified without logging their values.
func MigrateTokenEncryption(db *gorm.DB) error {
	db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)})
	if _, err := tokenCipher(); err != nil {
		return err
	}
	if err := db.AutoMigrate(&Token{}); err != nil {
		return ErrTokenStorage
	}
	var lastID int
	for {
		var rows []struct {
			Id        int
			Key       string
			KeyDigest *string
		}
		if err := db.Unscoped().Table("tokens").Where("id > ?", lastID).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return ErrTokenStorage
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			plain, err := decryptTokenKey(row.Key)
			// Migration accepts plaintext regardless of the service enable flag.
			if !strings.HasPrefix(row.Key, "enc1:") {
				plain, err = row.Key, nil
			}
			if err != nil {
				return err
			}
			digest := tokenDigest(plain)
			if strings.HasPrefix(row.Key, "enc1:") {
				if row.KeyDigest == nil || *row.KeyDigest != digest {
					return ErrTokenStorage
				}
			} else {
				sealed, err := encryptTokenKey(plain)
				if err != nil {
					return err
				}
				result := db.Unscoped().Table("tokens").Where("id = ?", row.Id).Where(clause.Eq{Column: clause.Column{Name: "key"}, Value: row.Key}).Updates(map[string]any{"key": sealed, "key_digest": digest})
				if result.Error != nil || result.RowsAffected != 1 {
					return ErrTokenStorage
				}
			}
			lastID = row.Id
		}
	}
}

func TokenEncryptionReady(db *gorm.DB) error {
	if !TokenEncryptionEnabled() {
		return ErrTokenStorage
	}
	if _, err := tokenCipher(); err != nil {
		return err
	}
	if !db.Migrator().HasIndex(&Token{}, "idx_token_digest") {
		return ErrTokenStorage
	}
	// Validate every row in bounded batches: no plaintext or wrong-key fallback.
	var lastID int
	for {
		var rows []Token
		if err := db.Unscoped().Where("id > ?", lastID).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return ErrTokenStorage
		}
		if len(rows) == 0 {
			return nil
		}
		for _, token := range rows {
			if token.KeyDigest == nil || *token.KeyDigest != tokenDigest(token.Key) {
				return ErrTokenStorage
			}
			lastID = token.Id
		}
	}
}

// Rotation runs offline and is resumable. The new key file is configured via
// CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE; the retired file is supplied separately.
// Already rotated rows verify with the new key, never return to plaintext.
func RotateTokenEncryption(db *gorm.DB) error {
	if !TokenEncryptionEnabled() {
		return ErrTokenStorage
	}
	db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)})
	oldData, err := os.ReadFile(os.Getenv("CLEARAPI_TOKEN_ENCRYPTION_OLD_KEY_FILE"))
	if err != nil || len(oldData) > 128 {
		return ErrTokenStorage
	}
	oldKey, err := hex.DecodeString(strings.TrimSpace(string(oldData)))
	if err != nil || len(oldKey) != 32 {
		return ErrTokenStorage
	}
	block, err := aes.NewCipher(oldKey)
	if err != nil {
		return ErrTokenStorage
	}
	oldCipher, err := cipher.NewGCM(block)
	if err != nil {
		return ErrTokenStorage
	}
	if _, err := tokenCipher(); err != nil {
		return err
	}
	var lastID int
	for {
		var rows []struct {
			Id        int
			Key       string
			KeyDigest *string
		}
		if err := db.Unscoped().Table("tokens").Where("id > ?", lastID).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return ErrTokenStorage
		}
		if len(rows) == 0 {
			return TokenEncryptionReady(db)
		}
		for _, row := range rows {
			plain, err := decryptTokenKey(row.Key)
			if err != nil {
				data, decodeErr := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(row.Key, "enc1:"))
				if decodeErr != nil || !strings.HasPrefix(row.Key, "enc1:") || len(data) < oldCipher.NonceSize()+oldCipher.Overhead() {
					return ErrTokenStorage
				}
				clear, err := oldCipher.Open(nil, data[:oldCipher.NonceSize()], data[oldCipher.NonceSize():], []byte("clearapi-api-token-v1"))
				if err != nil {
					return ErrTokenStorage
				}
				plain = string(clear)
				if row.KeyDigest == nil || *row.KeyDigest != tokenDigest(plain) {
					return ErrTokenStorage
				}
				sealed, err := encryptTokenKey(plain)
				if err != nil {
					return err
				}
				result := db.Unscoped().Table("tokens").Where("id = ?", row.Id).Where(clause.Eq{Column: clause.Column{Name: "key"}, Value: row.Key}).Update("key", sealed)
				if result.Error != nil || result.RowsAffected != 1 {
					return ErrTokenStorage
				}
			}
			if row.KeyDigest == nil || *row.KeyDigest != tokenDigest(plain) {
				return ErrTokenStorage
			}
			lastID = row.Id
		}
	}
}
