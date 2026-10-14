package bootstrap

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// EnvIssue is a single environment finding; WARNING-level findings do not fail
// the run but are always reported.
type EnvIssue struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// productionEnvPairs mirrors the exact requirements enforced by
// service.ValidateProductionReadiness so misconfiguration surfaces before any
// write happens instead of after migrations have run.
var productionEnvPairs = map[string]string{
	"GIN_MODE":                "release",
	"SESSION_COOKIE_SECURE":   "true",
	"CLEARAPI_DURABLE_WALLET": "true",
	"CLEARAPI_BILLING_OUTBOX": "true",
	"CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE": "true",
	"CLEARAPI_TASK_SETTLEMENT":                        "true",
	"CLEARAPI_STRICT_PREPAID":                         "true",
	"CLEARAPI_TOKEN_ENCRYPTION":                       "true",
	"CLEARAPI_DEPLOYMENT_INSTANCES":                   "1",
	"CLEARAPI_ENABLE_REALTIME":                        "false",
}

var productionFalseEnv = []string{"DEBUG", "DIFY_DEBUG", "ENABLE_PPROF", "BATCH_UPDATE_ENABLED"}

var relayLimitEnv = []string{
	"MAX_UPSTREAM_RESPONSE_MB", "MAX_REQUEST_BODY_MB",
	"RELAY_CONCURRENCY_GLOBAL", "RELAY_CONCURRENCY_USER",
	"RELAY_CONCURRENCY_TOKEN", "RELAY_CONCURRENCY_IP",
}

// IsPostgresDSN reports whether the DSN explicitly selects PostgreSQL.
func IsPostgresDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// CheckSQLDSN enforces the production dialect contract: an explicit
// PostgreSQL DSN is mandatory because an empty SQL_DSN silently falls back to
// SQLite and would bootstrap a throwaway database.
func CheckSQLDSN() error {
	if !IsPostgresDSN(os.Getenv("SQL_DSN")) {
		return errors.New("SQL_DSN must be an explicit postgres:// or postgresql:// DSN (empty values silently fall back to SQLite and are rejected)")
	}
	return nil
}

// CheckProductionEnv returns every environment finding. It is informational
// for the caller; strictness is decided by the mode.
func CheckProductionEnv() []EnvIssue {
	var issues []EnvIssue
	add := func(name, detail string) {
		issues = append(issues, EnvIssue{Name: name, Detail: detail})
	}
	for name, expected := range productionEnvPairs {
		if os.Getenv(name) != expected {
			add(name, "must be "+expected)
		}
	}
	for _, name := range productionFalseEnv {
		if os.Getenv(name) != "false" {
			add(name, "must be explicitly \"false\" (unset values are rejected)")
		}
	}
	if len(os.Getenv("SESSION_SECRET")) < 32 {
		add("SESSION_SECRET", "production requires an explicit persistent session secret of at least 32 bytes")
	}
	if os.Getenv("CLEARAPI_FINANCE_ENABLED") != "true" {
		add("CLEARAPI_FINANCE_ENABLED", "must be true")
	}
	if origin, err := url.Parse(os.Getenv("SESSION_COOKIE_TRUSTED_URL")); err != nil ||
		origin.Scheme != "https" || origin.Host == "" || origin.User != nil ||
		origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		add("SESSION_COOKIE_TRUSTED_URL", "must be an exact HTTPS origin")
	}
	if strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")) == "" {
		add("TRUSTED_PROXIES", "production trusted proxies must be explicit (use \"none\" when no reverse proxy exists)")
	}
	for _, name := range relayLimitEnv {
		n, err := strconv.Atoi(os.Getenv(name))
		if err != nil || n < 1 || n > 128 {
			add(name, "must be an integer in [1,128]")
		}
	}
	if os.Getenv("CRYPTO_SECRET") == "" {
		add("CRYPTO_SECRET", "WARNING: not set; it falls back to SESSION_SECRET (independence is recommended)")
	}
	return issues
}

// CheckTokenKeyFile validates the token encryption key file the same way the
// runtime does (at most 128 bytes, hex, exactly 32 key bytes).
func CheckTokenKeyFile() error {
	path := os.Getenv("CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE")
	if path == "" {
		return errors.New("CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("token encryption key file: %w", err)
	}
	if info.Size() > 128 {
		return errors.New("token encryption key file must be at most 128 bytes")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("token encryption key file: %w", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != 32 {
		return errors.New("token encryption key file must decode from hex to exactly 32 bytes")
	}
	return nil
}

// maxLegalFileBytes bounds legal file inputs (2 MiB).
const maxLegalFileBytes = 2 << 20

// readSecretFile reads a password file in one shot. Trailing newlines (a
// common artifact of file authoring) are stripped; the value never appears in
// logs, argv or environment.
func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return "", fmt.Errorf("secret file %s is empty", path)
	}
	return value, nil
}

// readLegalInput loads one legal file and enforces the input contract:
// valid UTF-8, non-empty after trimming, bounded size.
func readLegalInput(path string) (string, error) {
	if path == "" {
		return "", errors.New("legal file path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("legal file: %w", err)
	}
	if info.Size() > maxLegalFileBytes {
		return "", fmt.Errorf("legal file %s exceeds the %d byte limit", path, maxLegalFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("legal file: %w", err)
	}
	content := string(data)
	if !utf8.ValidString(content) {
		return "", fmt.Errorf("legal file %s is not valid UTF-8", path)
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("legal file %s is empty", path)
	}
	return content, nil
}
