package model

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	sqlitedriver "github.com/glebarez/go-sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Scope identity is the exact stored UTF-8 model/group pair. Lengths are byte
// counts encoded as unsigned big-endian uint64s, so delimiters cannot collide.
func routeChainActiveScopeKey(modelName, groupName string) string {
	hash := sha256.New()
	for _, part := range []string{modelName, groupName} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// The hook also protects internal fixture/import creates, not just management
// creates. Archive is a separate CAS operation and never goes through Create.
func (chain *RouteChain) BeforeCreate(_ *gorm.DB) error {
	if chain.Archived {
		chain.Enabled = false
		chain.ActiveScopeKey = nil
	} else {
		key := routeChainActiveScopeKey(chain.Model, chain.Group)
		chain.ActiveScopeKey = &key
	}
	return nil
}

func routeChainScopeError(err error) error {
	var myErr *mysql.MySQLError
	var pgErr *pgconn.PgError
	var sqliteErr *sqlitedriver.Error
	if errors.Is(err, gorm.ErrDuplicatedKey) ||
		(errors.As(err, &myErr) && myErr.Number == 1062 && strings.Contains(myErr.Message, "idx_route_chain_active_scope")) ||
		(errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_route_chain_active_scope") ||
		(errors.As(err, &sqliteErr) && sqliteErr.Code() == 2067 && strings.Contains(sqliteErr.Error(), "active_scope_key")) {
		return ErrRouteChainExists
	}
	return err
}

// MigrateRouteChain runs at startup before accepting management writes. MySQL
// DDL is not transactional: retain the legacy index until the new index and
// committed backfill are verified. An interrupted migration is safe to rerun.
func MigrateRouteChain(db *gorm.DB) error {
	if db.Migrator().HasTable(&RouteChain{}) {
		var rows []RouteChain
		columns := []string{"id", "model", "group"}
		if db.Migrator().HasColumn(&RouteChain{}, "archived") {
			columns = append(columns, "archived")
		}
		if err := db.Select(columns).Find(&rows).Error; err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, row := range rows {
			if row.Archived {
				continue
			}
			key := routeChainActiveScopeKey(row.Model, row.Group)
			if seen[key] {
				return fmt.Errorf("route chain migration: duplicate non-archived scope: %w", ErrRouteChainExists)
			}
			seen[key] = true
		}
		if db.Migrator().HasIndex(&RouteChain{}, "idx_route_chain_active_scope") {
			if err := verifyRouteChainActiveScopeIndex(db); err != nil {
				return err
			}
		}
	}
	if err := db.AutoMigrate(&RouteChain{}, &RouteChainCandidate{}); err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var rows []RouteChain
		if err := lockForUpdate(tx).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			var key *string
			if !row.Archived {
				value := routeChainActiveScopeKey(row.Model, row.Group)
				key = &value
			}
			if (key == nil && row.ActiveScopeKey == nil) ||
				(key != nil && row.ActiveScopeKey != nil && *key == *row.ActiveScopeKey) {
				continue
			}
			if err := tx.Model(&RouteChain{}).Where("id = ?", row.Id).
				Update("active_scope_key", key).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := verifyRouteChainActiveScopeIndex(db); err != nil {
		return err
	}
	if db.Migrator().HasIndex(&RouteChain{}, "idx_route_chain_model_group") {
		return db.Migrator().DropIndex(&RouteChain{}, "idx_route_chain_model_group")
	}
	return nil
}

func verifyRouteChainActiveScopeIndex(db *gorm.DB) error {
	indexes, err := db.Migrator().GetIndexes(&RouteChain{})
	if err != nil {
		return err
	}
	for _, index := range indexes {
		unique, known := index.Unique()
		columns := index.Columns()
		if index.Name() == "idx_route_chain_active_scope" && known && unique &&
			len(columns) == 1 && columns[0] == "active_scope_key" {
			return nil
		}
	}
	return errors.New("route chain migration: active scope unique index missing or invalid")
}
