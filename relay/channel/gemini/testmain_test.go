package gemini

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// gin.SetMode mutates a process-global; per-test calls from t.Parallel tests
// race with each other. Set it once before any test runs.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
