package accountpool

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelAdmissionPinnedWebMeteringAndOAuthGapStayClosed(t *testing.T) {
	grok := ModelAdmissionFor("grok_web_sso", "cookie_import", "unregistered-model")
	require.False(t, grok.Open)
	require.Equal(t, "local_estimate", grok.MeteringSource)
	require.Contains(t, grok.Reasons, "web_usage_is_estimated")
	require.Error(t, grok.Check([]string{"exact_usage"}, AdapterVersion))
	for _, capability := range grok.Capabilities {
		if capability.Capability == "exact_usage" {
			require.Equal(t, "unsupported", capability.Status)
			require.Len(t, capability.Evidence, 1)
			require.Equal(t, "source", capability.Evidence[0].Kind)
		}
	}
	gemini := ModelAdmissionFor("gemini_code_assist", "oauth_browser", "unregistered-model")
	require.False(t, gemini.Open)
	require.Contains(t, gemini.Reasons, "code_assist_oauth_executor_missing")
	require.Error(t, gemini.Check([]string{"text_chat"}, AdapterVersion))
}

func TestModelAdmissionUnknownVersionAndSimulatedTools(t *testing.T) {
	for _, platform := range []string{"codex", "grok", "gemini", "claude", "kimi", "qwen", "deepseek"} {
		entry := ModelAdmissionFor(platform, "oauth", "unregistered-model")
		if entry.Open || len(entry.Capabilities) != 7 || entry.Check([]string{"text_chat"}, AdapterVersion) == nil {
			t.Fatal(platform, entry)
		}
		if entry.Check(nil, AdapterVersion+1) == nil {
			t.Fatal("stale version accepted")
		}
	}
	entry := ModelAdmission{AdapterVersion: AdapterVersion, Capabilities: []ModelCapability{{Capability: "tools", Status: "supported", Native: false, Evidence: []CapabilityEvidence{{Kind: "source", Reference: "fixed", Revision: "1", AdapterVersion: AdapterVersion}, {Kind: "test", Reference: "fixed", Revision: "1", AdapterVersion: AdapterVersion}}}}}
	if err := entry.Check([]string{"tools"}, AdapterVersion); err == nil || err == ErrPoolIsolationReq {
		t.Fatal("prompt tools admitted", err)
	}
	entry.Capabilities[0].Native = true
	if err := entry.Check([]string{"tools"}, AdapterVersion); err != ErrPoolIsolationReq {
		t.Fatal("capability bypassed isolation", err)
	}
}
