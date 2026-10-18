package accountpool

import (
	"encoding/json"
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
	assert.Equal(t, ImportFormatCPA, credential.Format)
	assert.Equal(t, "codex:synthetic-a", credential.IdentityKey)
	assert.NotEmpty(t, credential.Digest)
	assert.NotContains(t, credential.IdentityMask, "synthetic-a")
}

func TestValidateCodexCredentialNestedCLI(t *testing.T) {
	raw := []byte(`{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","id_token":"synthetic-id","account_id":"cli-account","chatgpt_account_id":"cli-account","email":"cli@example.com","plan_type":"plus"},"last_refresh":"2026-10-18T00:00:00Z"}`)
	credential, err := ValidateCodexCredential(raw)
	require.NoError(t, err)
	assert.Equal(t, ImportFormatCodexCLI, credential.Format)
	assert.Equal(t, "codex:cli-account", credential.IdentityKey)
	assert.Equal(t, "cli@example.com", credential.Email)
	assert.NotContains(t, credential.IdentityMask, "cli@example.com")
	var converted map[string]any
	require.NoError(t, json.Unmarshal(credential.ExecutorRaw, &converted))
	assert.Equal(t, "codex", converted["type"])
	assert.Equal(t, "cli-account", converted["account_id"])
	assert.Equal(t, "cli-account", converted["chatgpt_account_id"])
	assert.Equal(t, "cli@example.com", converted["email"])
	assert.NotContains(t, converted, "tokens")
	assert.NotContains(t, converted, "OPENAI_API_KEY")
	assert.Equal(t, "synthetic-access", converted["access_token"])
	assert.Equal(t, "synthetic-refresh", converted["refresh_token"])
}

func TestValidateCodexCredentialRejectsUnsafeShapes(t *testing.T) {
	cases := map[string]string{
		"malformed":         `{"type":"codex"`,
		"duplicate":         `{"type":"codex","type":"codex"}`,
		"nested-collection": `{"type":"codex","auths":[]}`,
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
