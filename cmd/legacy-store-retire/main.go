// legacy-store-retire reviews, or explicitly applies, a separately approved
// withdrawal. It never starts the gateway or runs schema migrations.
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
	dialect := flag.String("dialect", "", "sqlite, mysql or postgres")
	actor := flag.Int("actor-id", 0, "reviewing active root operator ID")
	evidence := flag.String("evidence-ref", "", "non-secret reference to approved historical evidence")
	apply := flag.String("apply-approved-plan", "", "reviewed JSON plan file; requires separate production approval")
	confirmation := flag.String("confirmation", "", "exact RETIRE_LEGACY confirmation from the reviewed plan")
	flag.Parse()
	if flag.NArg() != 0 || (*apply == "" && (*actor <= 0 || *evidence == "" || *confirmation != "")) || (*apply != "" && (*actor != 0 || *evidence != "" || *confirmation == "")) {
		fmt.Fprintln(os.Stderr, "No operation executed. Select read-only review with actor-id/evidence-ref, or a separately approved plan with confirmation.")
		os.Exit(2)
	}
	dsn := os.Getenv("CLEARAPI_LEGACY_RETIREMENT_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "Retirement DSN is missing; no operation executed.")
		os.Exit(2)
	}
	var driver gorm.Dialector
	var databaseType common.DatabaseType
	switch *dialect {
	case "sqlite":
		driver, databaseType = sqlite.Open(dsn), common.DatabaseTypeSQLite
	case "mysql":
		driver, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
	case "postgres":
		driver, databaseType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
	default:
		fmt.Fprintln(os.Stderr, "Unsupported dialect; no operation executed.")
		os.Exit(2)
	}
	common.SetDatabaseTypes(databaseType, databaseType)
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database connection failed; details withheld.")
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database connection unavailable.")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var review struct {
		Plan         model.LegacyCreditRetirementPlan `json:"plan"`
		Confirmation string                           `json:"confirmation"`
	}
	if *apply == "" {
		review.Plan, err = model.ReviewLegacyCreditRetirement(ctx, db, *actor, *evidence)
		if err == nil {
			review.Confirmation, err = review.Plan.Confirmation()
		}
		if err == nil {
			var raw []byte
			raw, err = common.Marshal(review)
			if err == nil {
				fmt.Println(string(raw))
			}
		}
	} else {
		var raw []byte
		raw, err = os.ReadFile(*apply)
		if err == nil {
			err = common.Unmarshal(raw, &review)
		}
		if err == nil {
			err = model.RetireLegacyCreditCatalog(ctx, db, review.Plan, *confirmation)
		}
		if err == nil {
			fmt.Println("Approved legacy retirement committed or existing identical retirement verified.")
		}
	}
	closeErr := sqlDB.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Operation refused or failed; transaction not committed. Recheck the reviewed plan, switches, source history and operator. Database details withheld.")
		os.Exit(1)
	}
	if closeErr != nil {
		fmt.Fprintln(os.Stderr, "Operation completed; closing the database connection failed. Verify before retrying.")
		os.Exit(1)
	}
}
