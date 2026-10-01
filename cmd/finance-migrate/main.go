// finance-migrate is an explicit, additive migration command. The gateway does
// not call it at startup and never obtains its DSN from editable site options.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	dialect := flag.String("dialect", "", "sqlite, mysql or postgres")
	approved := flag.Bool("apply-approved-finance-v1", false, "apply only after separate migration approval and a verified backup")
	approvedV2 := flag.Bool("apply-approved-finance-v2", false, "apply source ledger only after separate v2 migration approval and a verified backup")
	approvedV3 := flag.Bool("apply-approved-finance-v3", false, "apply request observations only after separate v3 migration approval and a verified backup")
	approvedV4 := flag.Bool("apply-approved-finance-v4", false, "apply manual cash receipts only after separate v4 migration approval and a verified backup")
	approvedV5 := flag.Bool("apply-approved-finance-v5", false, "apply supplier bills only after separate v5 migration approval and a verified backup")
	flag.Parse()
	approvalCount := 0
	for _, choice := range []bool{*approved, *approvedV2, *approvedV3, *approvedV4, *approvedV5} {
		if choice {
			approvalCount++
		}
	}
	if approvalCount != 1 {
		fmt.Fprintln(os.Stderr, "No migration executed. Exactly one separately approved finance schema version is required.")
		os.Exit(2)
	}
	dsn := os.Getenv("CLEARAPI_FINANCE_MIGRATION_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "Migration DSN is missing; no migration executed.")
		os.Exit(2)
	}
	var driver gorm.Dialector
	switch *dialect {
	case "sqlite":
		driver = sqlite.Open(dsn)
	case "mysql":
		driver = mysql.Open(dsn)
	case "postgres":
		driver = postgres.Open(dsn)
	default:
		fmt.Fprintln(os.Stderr, "Unsupported dialect; no migration executed.")
		os.Exit(2)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database connection failed; migration not executed.")
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database connection unavailable.")
		os.Exit(1)
	}
	migrate := model.MigrateFinance
	version := 1
	if *approvedV2 {
		migrate, version = model.MigrateFinanceRevenue, 2
	}
	if *approvedV3 {
		migrate, version = model.MigrateFinanceRequests, 3
	}
	if *approvedV4 {
		migrate, version = model.MigrateFinanceReceipts, 4
	}
	if *approvedV5 {
		migrate, version = model.MigrateFinanceSupplier, 5
	}
	if err := migrate(db); err != nil {
		_ = sqlDB.Close()
		fmt.Fprintln(os.Stderr, "Finance migration failed. Check the approved target and restore plan; database details were not printed.")
		os.Exit(1)
	}
	if err := sqlDB.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "Finance schema applied; connection close failed.")
		os.Exit(1)
	}
	fmt.Printf("Approved finance schema v%d applied; existing user balances were not migrated.\n", version)
}
