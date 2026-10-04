package accountpool

import "fmt"

type CapabilityEvidence struct {
	Kind           string `json:"kind"`
	Reference      string `json:"reference"`
	Revision       string `json:"revision"`
	AdapterVersion int    `json:"adapter_version"`
}
type ModelCapability struct {
	Capability string               `json:"capability"`
	Status     string               `json:"status"`
	Evidence   []CapabilityEvidence `json:"evidence"`
	Native     bool                 `json:"native"`
}
type ModelAdmission struct {
	Platform        string            `json:"platform"`
	AccessType      string            `json:"access_type"`
	Model           string            `json:"model"`
	AdapterVersion  int               `json:"adapter_version"`
	Capabilities    []ModelCapability `json:"capabilities"`
	Open            bool              `json:"open"`
	Reasons         []string          `json:"reasons"`
	MeteringSource  string            `json:"metering_source"`
	QuotaCapability string            `json:"quota_capability"`
}

var modelCapabilityKeys = []string{"text_chat", "stream_text", "tools", "image_input", "image_generation", "long_context", "exact_usage"}

// ModelAdmissionFor never guesses capability support from a provider/model
// name. Evidence records may be added in code review; unregistered models get
// the same explicit closed state. Status and evidence are independent.
func ModelAdmissionFor(platform, accessType, modelName string) ModelAdmission {
	result := ModelAdmission{Platform: platform, AccessType: accessType, Model: modelName, AdapterVersion: AdapterVersion,
		Open: false, Reasons: []string{"continuous_isolation_unverified", "model_capabilities_unverified", "exact_metering_unverified"},
		MeteringSource: "unknown", QuotaCapability: "unsupported", Capabilities: []ModelCapability{}}
	_, access, found := Lookup(platform, accessType)
	if !found || access.Disabled {
		result.Reasons = append(result.Reasons, "access_not_admitted")
	}
	if platform == "codex" {
		result.QuotaCapability = "observations_only"
	}
	if found && access.ExecutorId == ExecutorCliproxyAPI {
		result.MeteringSource = "executor_compatibility_usage_without_field_presence_proof"
	}
	for _, capability := range modelCapabilityKeys {
		evidence := []CapabilityEvidence{}
		if found && access.ExecutorId == ExecutorCliproxyAPI && capability == "exact_usage" {
			evidence = append(evidence, CapabilityEvidence{Kind: "source", Reference: "CLIProxyAPI/internal/runtime/executor/helps/usage_helpers.go; new-api/relay/channel/openai/relay-openai.go",
				Revision: "CLIProxyAPI:d33f63f8e3d98428440ebca5a5b6a981a61ff71e", AdapterVersion: AdapterVersion})
		}
		result.Capabilities = append(result.Capabilities, ModelCapability{Capability: capability, Status: "unverified", Evidence: evidence})
	}
	return result
}

// Check rejects stale evidence, prompt-emulated tools, and every unverified
// capability. Even verified capabilities cannot override the isolation gate.
func (admission ModelAdmission) Check(requested []string, adapterVersion int) error {
	if admission.AdapterVersion != adapterVersion {
		return fmt.Errorf("adapter_version_changed")
	}
	for _, key := range requested {
		allowed := false
		for _, capability := range admission.Capabilities {
			if capability.Capability != key || capability.Status != "supported" {
				continue
			}
			source, test := false, false
			for _, evidence := range capability.Evidence {
				if evidence.AdapterVersion != adapterVersion || evidence.Reference == "" || evidence.Revision == "" {
					continue
				}
				source = source || evidence.Kind == "source"
				test = test || evidence.Kind == "test"
			}
			allowed = source && test && (key != "tools" || capability.Native)
		}
		if !allowed {
			return fmt.Errorf("capability_not_admitted:%s", key)
		}
	}
	return ErrPoolIsolationReq
}
