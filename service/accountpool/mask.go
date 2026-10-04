package accountpool

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// MaskIdentity projects an upstream account identity into a stable,
// non-reversible display form. Masked values are FOR DISPLAY ONLY — equality
// of masked values says nothing about account equality; use
// IdentityFingerprint for that decision.
func MaskIdentity(identity string) string {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ""
	}
	if at := strings.Index(identity, "@"); at > 0 {
		return maskHead(identity[:at]) + "@" + maskHead(identity[at+1:])
	}
	return maskHead(identity)
}

func maskHead(text string) string {
	runes := []rune(text)
	if len(runes) <= 1 {
		return "*"
	}
	return string(runes[:1]) + "***"
}

// IdentityFingerprint is the server-side stable account identity: a SHA-256
// of the normalized upstream identity. Only the fingerprint (never the raw
// identity) is persisted and compared for re-auth matching.
func IdentityFingerprint(identity string) string {
	normalized := strings.ToLower(strings.TrimSpace(identity))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}
