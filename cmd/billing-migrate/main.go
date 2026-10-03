// billing-migrate never runs during application startup.
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
	approved := flag.Bool("apply-approved-billing-v1", false, "requires a separately approved target and verified backup")
	reconciliation := flag.Bool("apply-approved-billing-reconciliation-v1", false, "separate approval for review/decision tables; requires existing finance v3 or newer")
	tasks := flag.Bool("apply-approved-task-settlement-v1", false, "separate approval for the task settlement intent/outbox table")
	taskLogs := flag.Bool("apply-approved-task-settlement-logs-v1", false, "separate approval for log-database delivery receipts")
	flag.Parse()
	dsn := os.Getenv("CLEARAPI_BILLING_MIGRATION_DSN")
	approvals := 0
	for _, selected := range []bool{*approved, *reconciliation, *tasks, *taskLogs} {
		if selected {
			approvals++
		}
	}
	if approvals != 1 || dsn == "" {
		fmt.Fprintln(os.Stderr, "No migration executed: approval flag and target DSN required.")
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
		fmt.Fprintln(os.Stderr, "No migration executed: unsupported dialect.")
		os.Exit(2)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Billing database unavailable.")
		os.Exit(1)
	}
	migrate := model.MigrateBillingReservations
	verify := model.BillingReservationsReady
	if *reconciliation {
		migrate, verify = model.MigrateBillingReconciliation, model.BillingReconciliationReady
	}
	if *tasks {
		migrate, verify = model.MigrateTaskSettlements, model.TaskSettlementSchemaReady
	}
	if *taskLogs {
		migrate, verify = model.MigrateTaskSettlementLogs, model.TaskSettlementLogsReady
	}
	if err := migrate(db); err != nil {
		fmt.Fprintln(os.Stderr, "Billing migration failed; inspect the approved target and restore plan.")
		os.Exit(1)
	}
	if err := verify(db); err != nil {
		fmt.Fprintln(os.Stderr, "Billing schema verification failed.")
		os.Exit(1)
	}
	fmt.Println("Approved billing schema applied. Existing balances were not migrated.")
}
