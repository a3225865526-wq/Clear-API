package accountpool

import "testing"

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
