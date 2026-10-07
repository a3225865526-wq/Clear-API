package operation_setting

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// Reserve policy modes. "off" is the default feature gate: the reservation
// hardening is fully disabled and the frozen candidate behavior is preserved.
// "monitor" resolves profiles and records the would-be decision without
// enforcing it. "enforce" rejects before dispatch on quarantine or cap breach.
const (
	ReserveModeOff     = "off"
	ReserveModeMonitor = "monitor"
	ReserveModeEnforce = "enforce"

	ReserveUnknownQuarantine = "quarantine"

	// CacheModeCold treats every hidden token as ordinary input: no cache hit
	// is assumed and the full hidden total is priced at input price.
	CacheModeCold = "cold"
	// CacheModeWarm approves a typed hidden split (read/write separately).
	CacheModeWarm = "warm"
)

// BillingReserveProfile is one owner-approved hidden usage envelope for a
// channel, a model, or an exact channel+model pair.
type BillingReserveProfile struct {
	Approved              bool    `json:"approved"`
	Version               int     `json:"version"`
	CacheMode             string  `json:"cache_mode"`
	HiddenTotalMax        int64   `json:"hidden_total_max"`
	HiddenInputMax        int64   `json:"hidden_input_max"`
	HiddenCacheReadMax    int64   `json:"hidden_cache_read_max"`
	HiddenCacheWriteMax   int64   `json:"hidden_cache_write_max"`
	HiddenCacheWrite1hMax int64   `json:"hidden_cache_write_1h_max"`
	HiddenRelativeLimit   float64 `json:"hidden_relative_limit"`
	ModelContextMax       int64   `json:"model_context_max"`
	ModelOutputMax        int64   `json:"model_output_max"`
	MaxRequestQuota       int64   `json:"max_request_quota"`
}

// BillingReservePolicySetting is registered under the
// "billing_reserve_policy" config module. The profiles map persists as one
// JSON option value (billing_reserve_policy.profiles). Field updates flow
// through the option registry's synchronized update path, matching the
// quota_setting pattern; readers copy the struct once per request.
type BillingReservePolicySetting struct {
	Version         int                              `json:"version"`
	Mode            string                           `json:"mode"`
	UnknownAction   string                           `json:"unknown_action"`
	Margin          float64                          `json:"margin"`
	FloorQuota      int64                            `json:"floor_quota"`
	MaxRequestQuota int64                            `json:"max_request_quota"`
	Profiles        map[string]BillingReserveProfile `json:"profiles"`
}

var billingReservePolicy = BillingReservePolicySetting{
	Mode:            ReserveModeOff,
	UnknownAction:   ReserveUnknownQuarantine,
	Margin:          1.10,
	FloorQuota:      0,
	MaxRequestQuota: 5000,
	Profiles:        map[string]BillingReserveProfile{},
}

func init() {
	config.GlobalConfig.Register("billing_reserve_policy", &billingReservePolicy)
}

// GetBillingReservePolicy returns a snapshot of the current policy. Callers
// must treat the returned value as frozen for the current request.
func GetBillingReservePolicy() BillingReservePolicySetting {
	policy := billingReservePolicy
	if policy.Profiles == nil {
		policy.Profiles = map[string]BillingReserveProfile{}
	}
	return policy
}

// ReservationHardeningMode reports the active feature-gate mode.
func ReservationHardeningMode() string {
	switch billingReservePolicy.Mode {
	case ReserveModeMonitor, ReserveModeEnforce:
		return billingReservePolicy.Mode
	default:
		return ReserveModeOff
	}
}

// EffectiveMarginFloor returns the margin and floor quota defaults. An invalid
// margin fails closed (treated as off) instead of silently reserving 0.
func EffectiveMarginFloor(policy BillingReservePolicySetting) (float64, int64, error) {
	margin, floor := policy.Margin, policy.FloorQuota
	if math.IsNaN(margin) || math.IsInf(margin, 0) || margin < 1 || margin > 2 {
		return 0, 0, fmt.Errorf("billing_reserve_policy margin must be between 1 and 2")
	}
	if floor < 0 || floor > policy.MaxRequestQuota {
		return 0, 0, fmt.Errorf("billing_reserve_policy floor_quota must be between 0 and max_request_quota")
	}
	return margin, floor, nil
}

// profileKey helpers define the precedence: exact channel+model profile wins
// over channel profile and model profile.
func ChannelModelProfileKey(channelID int, model string) string {
	return fmt.Sprintf("channel::%d::model::%s", channelID, model)
}

func ChannelProfileKey(channelID int) string {
	return fmt.Sprintf("channel::%d", channelID)
}

func ModelProfileKey(model string) string {
	return fmt.Sprintf("model::%s", model)
}

// ResolvedReserveProfile is the approved envelope for one dispatch candidate.
// Cap fields already carry the min() across the matching chain and the global
// policy, because overrides may tighten but never widen a cap.
type ResolvedReserveProfile struct {
	Source                string
	Version               int
	CacheMode             string
	HiddenTotalMax        int64
	HiddenInputMax        int64
	HiddenCacheReadMax    int64
	HiddenCacheWriteMax   int64
	HiddenCacheWrite1hMax int64
	HiddenRelativeLimit   float64
	ModelContextMax       int64
	ModelOutputMax        int64
	MaxRequestQuota       int64
}

// ResolveReserveProfile finds the most specific approved profile for a
// dispatch candidate (channel id + final upstream model). Profiles that are
// not approved do not exist for admission purposes: an unknown candidate must
// quarantine instead of silently reserving an unbounded amount.
func ResolveReserveProfile(policy BillingReservePolicySetting, channelID int, model string) (*ResolvedReserveProfile, bool) {
	ordered := []string{
		ChannelModelProfileKey(channelID, model),
		ChannelProfileKey(channelID),
		ModelProfileKey(model),
	}
	for _, key := range ordered {
		profile, ok := policy.Profiles[key]
		if !ok || !profile.Approved {
			continue
		}
		NormalizeReserveProfile(&profile)
		resolved := &ResolvedReserveProfile{
			Source:                key,
			Version:               profile.Version,
			CacheMode:             profile.CacheMode,
			HiddenTotalMax:        profile.HiddenTotalMax,
			HiddenInputMax:        profile.HiddenInputMax,
			HiddenCacheReadMax:    profile.HiddenCacheReadMax,
			HiddenCacheWriteMax:   profile.HiddenCacheWriteMax,
			HiddenCacheWrite1hMax: profile.HiddenCacheWrite1hMax,
			HiddenRelativeLimit:   profile.HiddenRelativeLimit,
			ModelContextMax:       profile.ModelContextMax,
			ModelOutputMax:        profile.ModelOutputMax,
		}
		// Caps can only tighten: the most specific profile starts from the
		// global cap and every override below shrinks it.
		resolved.MaxRequestQuota = policy.MaxRequestQuota
		if profile.MaxRequestQuota > 0 && profile.MaxRequestQuota < resolved.MaxRequestQuota {
			resolved.MaxRequestQuota = profile.MaxRequestQuota
		}
		return resolved, true
	}
	return nil, false
}

// NormalizeReserveProfile fills a cold profile's typed fields from the single
// hidden total: with unknown cache behavior every hidden token must be priced
// as ordinary input, so the read/write allowances collapse to zero.
func NormalizeReserveProfile(profile *BillingReserveProfile) {
	if profile.CacheMode != CacheModeCold && profile.CacheMode != CacheModeWarm {
		profile.CacheMode = CacheModeCold
	}
	if profile.CacheMode == CacheModeCold {
		if profile.HiddenInputMax <= 0 && profile.HiddenTotalMax > 0 {
			profile.HiddenInputMax = profile.HiddenTotalMax
		}
		if profile.HiddenTotalMax <= 0 {
			profile.HiddenTotalMax = profile.HiddenInputMax
		}
		profile.HiddenCacheReadMax = 0
		profile.HiddenCacheWriteMax = 0
		profile.HiddenCacheWrite1hMax = 0
	}
	if profile.HiddenTotalMax <= 0 {
		profile.HiddenTotalMax = profile.HiddenInputMax + profile.HiddenCacheReadMax + profile.HiddenCacheWriteMax + profile.HiddenCacheWrite1hMax
	}
	if profile.HiddenRelativeLimit <= 0 || profile.HiddenRelativeLimit > 1 {
		profile.HiddenRelativeLimit = 0.10
	}
}

// ValidateBillingReserveOption rejects invalid policy saves. It is wired into
// the option validation chain so an operator cannot persist a policy that
// would disable the prepaid boundary.
func ValidateBillingReserveOption(key, value string) error {
	if key != "billing_reserve_policy.mode" && key != "billing_reserve_policy.margin" &&
		key != "billing_reserve_policy.floor_quota" && key != "billing_reserve_policy.max_request_quota" &&
		key != "billing_reserve_policy.unknown_action" && key != "billing_reserve_policy.profiles" {
		return nil
	}
	switch key {
	case "billing_reserve_policy.mode":
		if value != ReserveModeOff && value != ReserveModeMonitor && value != ReserveModeEnforce {
			return fmt.Errorf("billing_reserve_policy.mode must be one of off/monitor/enforce")
		}
	case "billing_reserve_policy.unknown_action":
		if value != ReserveUnknownQuarantine {
			return fmt.Errorf("billing_reserve_policy.unknown_action only supports quarantine")
		}
	case "billing_reserve_policy.margin":
		number, err := parseFiniteFloat(value)
		if err != nil || number < 1 || number > 2 {
			return fmt.Errorf("billing_reserve_policy.margin must be a number between 1 and 2")
		}
	case "billing_reserve_policy.floor_quota", "billing_reserve_policy.max_request_quota":
		number, err := parseFiniteFloat(value)
		if err != nil || number < 0 || number > math.MaxInt32 {
			return fmt.Errorf("%s must be a non-negative integer below 2^31", key)
		}
	case "billing_reserve_policy.profiles":
		var profiles map[string]BillingReserveProfile
		if err := json.Unmarshal([]byte(value), &profiles); err != nil {
			return fmt.Errorf("billing_reserve_policy.profiles must be a JSON object: %w", err)
		}
		for name, profile := range profiles {
			if err := validateReserveProfile(name, profile); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateReserveProfile(name string, profile BillingReserveProfile) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("billing_reserve_policy.profiles keys must not be empty")
	}
	if !strings.HasPrefix(name, "channel::") && !strings.HasPrefix(name, "model::") {
		return fmt.Errorf("billing_reserve_policy.profiles key %q must start with channel:: or model::", name)
	}
	if !profile.Approved {
		return nil
	}
	if profile.CacheMode != CacheModeCold && profile.CacheMode != CacheModeWarm {
		return fmt.Errorf("billing_reserve_policy.profiles[%s].cache_mode must be cold or warm", name)
	}
	fields := []int64{profile.HiddenTotalMax, profile.HiddenInputMax, profile.HiddenCacheReadMax, profile.HiddenCacheWriteMax, profile.HiddenCacheWrite1hMax, profile.ModelContextMax, profile.ModelOutputMax, profile.MaxRequestQuota}
	for _, field := range fields {
		if field < 0 {
			return fmt.Errorf("billing_reserve_policy.profiles[%s] must not contain negative bounds", name)
		}
	}
	if profile.CacheMode == CacheModeWarm {
		total := profile.HiddenInputMax + profile.HiddenCacheReadMax + profile.HiddenCacheWriteMax + profile.HiddenCacheWrite1hMax
		if profile.HiddenTotalMax > 0 && total > profile.HiddenTotalMax {
			return fmt.Errorf("billing_reserve_policy.profiles[%s] typed hidden categories exceed hidden_total_max", name)
		}
	}
	if profile.HiddenRelativeLimit != 0 && (profile.HiddenRelativeLimit < 0 || profile.HiddenRelativeLimit > 1) {
		return fmt.Errorf("billing_reserve_policy.profiles[%s].hidden_relative_limit must be between 0 and 1", name)
	}
	return nil
}

func parseFiniteFloat(value string) (float64, error) {
	var number float64
	if err := json.Unmarshal([]byte(value), &number); err != nil {
		// Plain scalar strings are also accepted for operator convenience.
		if _, err2 := fmt.Sscanf(strings.TrimSpace(value), "%g", &number); err2 != nil {
			return 0, err
		}
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("not a finite number")
	}
	return number, nil
}
