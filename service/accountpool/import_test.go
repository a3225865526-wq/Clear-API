package accountpool

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func syntheticCodexJSON(account string) []byte {
	return []byte(`{"type":"codex","account_id":"` + account + `","chatgpt_account_id":"` + account + `","email":"` + account + `@example.com","plan_type":"plus","access_token":"synthetic-access","refresh_token":"synthetic-refresh","id_token":"synthetic-id"}`)
}

func TestValidateCodexCredentialTopLevel(t *testing.T) {
	credential, err := ValidateCodexCredential(syntheticCodexJSON("synthetic-a"))
	require.NoError(t, err)
	assert.Equal(t, ImportFormatCodexV1, credential.Format)
	assert.Equal(t, "codex:synthetic-a", credential.IdentityKey)
	assert.NotEmpty(t, credential.Digest)
	assert.NotContains(t, credential.IdentityMask, "synthetic-a")
}

func TestValidateCodexCredentialRejectsUnsafeShapes(t *testing.T) {
	cases := map[string]string{
		"malformed":         `{"type":"codex"`,
		"duplicate":         `{"type":"codex","type":"codex"}`,
		"nested":            `{"type":"codex","auths":[]}`,
		"provider":          `{"type":"unknown"}`,
		"missing-token":     `{"type":"codex","account_id":"a","email":"a@example.com","access_token":"x"}`,
		"missing-identity":  `{"type":"codex","email":"a@example.com","access_token":"x","refresh_token":"y"}`,
		"identity-mismatch": `{"type":"codex","account_id":"a","chatgpt_account_id":"b","email":"a@example.com","access_token":"x","refresh_token":"y"}`,
		"trailing":          `{"type":"codex","account_id":"a","email":"a@example.com","access_token":"x","refresh_token":"y"} {}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateCodexCredential([]byte(raw))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "synthetic")
		})
	}
}

func TestImportAdmissionRequiresTrustedDigestAndVerifiedModel(t *testing.T) {
	snapshot := PoolExecutorModelSnapshot{
		Identities: []PoolExecutorTrustIdentity{{AuthID: "a.json", Provider: "codex", CredentialDigest: "digest", State: "trusted"}},
	}
	admission, capability := importAdmission("digest", "a.json", snapshot, nil)
	assert.Equal(t, "trusted_no_model_grant", admission)
	assert.Equal(t, "unverified", capability)
	snapshot.Capabilities = []PoolExecutorCapability{{AuthID: "a.json", Provider: "codex", CredentialDigest: "digest", Model: "m", State: "verified"}}
	admission, capability = importAdmission("digest", "a.json", snapshot, nil)
	assert.Equal(t, "trusted", admission)
	assert.Equal(t, "verified", capability)
	admission, capability = importAdmission("wrong-digest", "a.json", snapshot, nil)
	assert.Equal(t, "pending_trust", admission)
	assert.True(t, strings.Contains(capability, "unverified"))
}
