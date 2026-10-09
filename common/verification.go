package common

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	digest   [32]byte
	time     time.Time
	attempts int
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10000
var VerificationValidMinutes = 10
var verificationSendTimes = make(map[string]time.Time)

// ReserveVerificationEmailSend applies an email-wide cooldown, including failed
// deliveries. IP throttling remains enforced by the existing middleware.
func ReserveVerificationEmailSend(email string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	now := time.Now()
	for key, sent := range verificationSendTimes {
		if now.Sub(sent) >= time.Minute {
			delete(verificationSendTimes, key)
		}
	}
	key := strings.ToLower(strings.TrimSpace(email))
	if _, exists := verificationSendTimes[key]; exists || len(verificationSendTimes) >= 10000 {
		return false
	}
	verificationSendTimes[key] = now
	return true
}

func GenerateEmailVerificationCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value), nil
}

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	if len(verificationMap) >= verificationMapMaxSize {
		removeExpiredPairs()
		if _, exists := verificationMap[purpose+key]; !exists && len(verificationMap) >= verificationMapMaxSize {
			return false
		}
	}
	verificationMap[purpose+key] = verificationValue{
		digest: sha256.Sum256([]byte(code)),
		time:   time.Now(),
	}
	return true
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	verified, _ := VerifyCodeAndRunWithKey(key, code, purpose, func() error { return nil })
	return verified
}

// VerifyCodeAndRunWithKey serializes proof verification with account creation.
// A failed database transaction keeps the code usable until its original expiry;
// only a committed operation consumes it. The callback must not call this cache.
func VerifyCodeAndRunWithKey(key string, code string, purpose string, apply func() error) (bool, error) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		delete(verificationMap, purpose+key)
		return false, nil
	}
	digest := sha256.Sum256([]byte(code))
	if subtle.ConstantTimeCompare(digest[:], value.digest[:]) == 1 {
		if err := apply(); err != nil {
			return true, err
		}
		delete(verificationMap, purpose+key)
		return true, nil
	}
	value.attempts++
	if value.attempts >= 5 {
		delete(verificationMap, purpose+key)
	} else {
		verificationMap[purpose+key] = value
	}
	return false, nil
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
