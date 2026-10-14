package bootstrap

import (
	"fmt"
	"os"

	"github.com/QuantumNous/new-api/common"
)

// Phase writes a human-readable progress line to stderr. stdout is reserved
// for the single machine-readable status line so automation can parse it.
func Phase(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[production-bootstrap] "+format+"\n", args...)
}

// Emit prints the final machine-readable status object to stdout.
func Emit(status string, fields map[string]any) {
	payload := make(map[string]any, len(fields)+1)
	for key, value := range fields {
		payload[key] = value
	}
	payload["status"] = status
	encoded, err := common.Marshal(payload)
	if err != nil {
		fmt.Printf("{\"status\":%q}\n", status)
		return
	}
	fmt.Println(string(encoded))
}
