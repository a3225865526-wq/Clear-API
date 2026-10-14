package main

import (
	"os"

	"github.com/QuantumNous/new-api/cmd/production-bootstrap/bootstrap"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := bootstrap.RegisterFlags()
	// common.InitEnv parses the shared flag set (which already contains the
	// bootstrap flags registered above) and initializes the same env-derived
	// globals as the server: finance/pool config, session cookie settings and
	// node identity. The bootstrap intentionally does not load a .env file:
	// its environment comes exclusively from the operator's launcher/compose
	// configuration so secret sourcing stays deterministic.
	common.InitEnv()
	logger.SetupLogger()
	return bootstrap.Dispatch(cfg)
}
