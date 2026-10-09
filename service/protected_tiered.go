package service

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// These reviewed token-only tariffs are the entire protected expression surface.
// Price corrections remain explicit operator configuration changes.
const ProtectedLunaCurrentExpr = `len <= 272000 ? tier("upstream_1", p * 0.02 + c * 0.1 + cr * 0.002 + cc1h * 0.025 + cc * 0.025) : tier("upstream_2", p * 0.04 + c * 0.15 + cc * 0.05 + cc1h * 0.05 + cr * 0.04)`
const ProtectedLunaCorrectedExpr = `len <= 272000 ? tier("upstream_1", p * 0.02 + c * 0.1 + cr * 0.002 + cc1h * 0.025 + cc * 0.025) : tier("upstream_2", p * 0.04 + c * 0.15 + cc * 0.05 + cc1h * 0.05 + cr * 0.004)`

// ValidateProtectedTieredSnapshot is also checked at wallet admission and at
// outbound validation. A profile or mode change cannot weaken the text gate.
func ValidateProtectedTieredSnapshot(info *relaycommon.RelayInfo) error {
	if info == nil || info.ChannelMeta == nil || info.TieredBillingSnapshot == nil {
		return fmt.Errorf("protected expression requires a selected channel and pricing snapshot")
	}
	snap := info.TieredBillingSnapshot
	if info.ChannelMeta.ChannelId != 1 || info.ChannelType != constant.ChannelTypeOpenAI ||
		info.GetOriginModelName() != "gpt-6-luna" || info.RelayMode != relayconstant.RelayModeChatCompletions || info.RelayFormat != types.RelayFormatOpenAI ||
		info.GetUpstreamModelName() != "gpt-6-luna" || snap.ModelName != "gpt-6-luna" ||
		info.UserGroup != "default" || info.UsingGroup != "default" || info.TokenGroup != "default" ||
		snap.BillingMode != "tiered_expr" || snap.TaskUsageBilling || snap.ExprVersion != 1 || snap.QuotaPerUnit != 500000 {
		return fmt.Errorf("protected expression is outside the approved channel/model/group")
	}
	if (snap.ExprString != ProtectedLunaCurrentExpr && snap.ExprString != ProtectedLunaCorrectedExpr) ||
		snap.ExprHash != billingexpr.ExprHashString(snap.ExprString) {
		return fmt.Errorf("protected expression tariff is not approved")
	}
	if snap.GroupRatio <= 0 || math.IsNaN(snap.GroupRatio) || math.IsInf(snap.GroupRatio, 0) {
		return fmt.Errorf("protected expression requires a finite positive group price")
	}
	policy := operation_setting.GetBillingReservePolicy()
	key := operation_setting.ChannelModelProfileKey(1, "gpt-6-luna")
	profile, exists := policy.Profiles[key]
	if policy.Mode != operation_setting.ReserveModeEnforce || policy.UnknownAction != operation_setting.ReserveUnknownQuarantine ||
		!exists || !profile.Approved || profile.Version < 1 || profile.ModelContextMax <= 0 ||
		profile.ModelOutputMax <= 0 || profile.MaxRequestQuota <= 0 || policy.MaxRequestQuota <= 0 {
		return fmt.Errorf("protected expression requires enforce and an approved bounded channel/model profile")
	}
	return nil
}
