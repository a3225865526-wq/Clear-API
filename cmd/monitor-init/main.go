// monitor-init never starts the gateway, a scheduler, or a provider adapter.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Monitor operation refused or failed; database credentials and errors are not printed.")
		os.Exit(1)
	}
}

func run() error {
	dialect := flag.String("dialect", "", "sqlite, mysql, postgres")
	database := flag.String("database", "", "explicit database name (or SQLite path)")
	migrate := flag.Bool("migrate", false, "inspect the dedicated monitoring migration")
	catalog := flag.String("catalog", "", "operator-approved JSON array of monitor configurations")
	apply := flag.Bool("apply", false, "execute rather than dry-run")
	confirmation := flag.String("confirm-database", "", "must match --database when applying")
	approveMigration := flag.Bool("approve-monitor-migration", false, "separate monitoring migration authorization")
	approvePublish := flag.Bool("approve-monitor-publication", false, "separate publication authorization")
	flag.Parse()
	if *database == "" || (!*migrate && *catalog == "") || (*apply && (*confirmation != *database || (*migrate && !*approveMigration) || (*catalog != "" && !*approvePublish))) {
		return model.ErrMonitorDenied
	}
	dsn := os.Getenv("CLEARAPI_MONITOR_DSN")
	if dsn == "" {
		return model.ErrMonitorInvalid
	}
	var driver gorm.Dialector
	switch *dialect {
	case "sqlite":
		common.SetMainDatabaseType(common.DatabaseTypeSQLite)
		// Prevent a default dry-run from creating a SQLite file.
		if dsn != *database {
			return model.ErrMonitorDenied
		}
		if _, err := os.Stat(dsn); err != nil {
			return err
		}
		driver = sqlite.Open(dsn)
	case "mysql":
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
		driver = mysql.Open(dsn)
	case "postgres":
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
		driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
	default:
		return model.ErrMonitorInvalid
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	if *dialect != "sqlite" {
		var name string
		query := "SELECT current_database()"
		if *dialect == "mysql" {
			query = "SELECT DATABASE()"
		}
		if err := db.Raw(query).Scan(&name).Error; err != nil {
			return err
		}
		if name != *database {
			return model.ErrMonitorDenied
		}
	}
	var targets []model.ConsoleMonitor
	if *catalog != "" {
		f, err := os.Open(*catalog)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := common.DecodeJson(f, &targets); err != nil {
			return err
		}
		if len(targets) == 0 || len(targets) > 100 {
			return model.ErrMonitorInvalid
		}
		for i := range targets {
			m := &targets[i]
			if m.ID != 0 || m.Enabled || m.Archived || !m.Published {
				return model.ErrMonitorDenied
			}
			// Approval is the exact supplied catalog, not all discovered channels.
			if err := model.ValidateMonitorTarget(db, m); err != nil {
				return err
			}
		}
	}
	if !*apply {
		fmt.Printf("DRY RUN: monitoring migration=%t, approved catalog entries=%d; no writes or probes executed.\n", *migrate, len(targets))
		return nil
	}
	if *migrate {
		if err := model.MigrateConsoleMonitoringDB(db); err != nil {
			return err
		}
	}
	if len(targets) > 0 {
		if !model.ConsoleMonitoringReady(db) {
			return model.ErrMonitorNotReady
		}
		// All catalog changes commit together or not at all.
		if err := db.Transaction(func(tx *gorm.DB) error {
			for i := range targets {
				m := &targets[i]
				m.Published = false
				if err := model.SaveConsoleMonitor(ctx, tx, m); err != nil {
					return err
				}
				if err := model.SetConsoleMonitorSwitch(ctx, tx, m.ID, "published", true); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	fmt.Println("Approved monitoring operation applied; no probes or scheduler were started.")
	return nil
}
