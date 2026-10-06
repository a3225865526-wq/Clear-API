package bootstrap

// Exit codes. ALREADY_BOOTSTRAPPED is a successful outcome (exit 0) and is
// distinguished only through the machine-readable status field: automation
// treats any non-zero exit as a failure and "already safely bootstrapped" is
// not a failure.
const (
	ExitOK                         = 0
	ExitInput                      = 2
	ExitEnvironment                = 3
	ExitFinanceOwnerConfigRequired = 7 // safe two-stage stop (Amendment 6), not a crash
	ExitMigration                  = 10
	ExitPopulatedDBRefusal         = 20
	ExitLegalMismatch              = 30
	ExitReadiness                  = 40
	ExitPersistence                = 50
	ExitCorruption                 = 60 // corrupt state/marker: fail closed, never auto-repair
)
