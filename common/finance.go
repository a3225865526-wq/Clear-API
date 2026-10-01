package common

import (
	"os"
	"strconv"
)

// Finance identity is process configuration, never an editable Option/Casbin grant.
var financeEnabled bool
var financeOwnerUserID int

func InitFinanceConfig() {
	financeEnabled = os.Getenv("CLEARAPI_FINANCE_ENABLED") == "true"
	financeOwnerUserID = 0
	id, err := strconv.Atoi(os.Getenv("CLEARAPI_FINANCE_OWNER_USER_ID"))
	if err == nil && id > 0 {
		financeOwnerUserID = id
	}
}

func FinanceEnabled() bool { return financeEnabled && financeOwnerUserID > 0 }

func IsFinanceOwner(userID int) bool {
	return FinanceEnabled() && userID == financeOwnerUserID
}
