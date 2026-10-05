package model

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// STEP 4 operator control plane. The previous unconditional pool-channel
// blocker is replaced by a layered release formula evaluated before dispatch:
//
//	account trusted AND capability verified (executor, STEP 1A/1B/3)
//	AND pool master switch on (this file / pool_master.go)
//	AND channel enabled (abilities)
//	AND channel explicitly allows pool routing (PoolRoutingEnabled)
//	AND model published for pool source (PoolModelRoute, pool_routing.go)
//	AND user/group/token may use the model (abilities + token limits)
//
// Any layer not explicitly allowing denies. Nothing here grants trust or
// capability, and capability never becomes publication.
var (
	ErrPoolGloballyDisabled      = errors.New("POOL_GLOBALLY_DISABLED: the account pool master switch is closed")
	ErrPoolChannelRoutingOff     = errors.New("POOL_CHANNEL_DISABLED: the channel does not allow account pool routing")
	ErrPoolModelNotPublished     = errors.New("MODEL_SOURCE_NOT_PUBLISHED: the model is not published for the account pool")
	ErrPoolRoutingRequiresBinder = errors.New("POOL_ROUTING_UNBOUND: pool routing requires a pool-bound channel")
)

// PoolChannelBound detects both explicit bindings and an executor authority.
// This prevents removing the binding marker from bypassing the gate when the
// configured management and inference endpoints share the executor instance.
func (channel *Channel) PoolChannelBound() bool {
	if channel == nil {
		return false
	}
	setting := ""
	if channel.Setting != nil {
		setting = *channel.Setting
	}
	for _, settings := range []string{channel.OtherSettings, setting} {
		var raw map[string]any
		if common.UnmarshalJsonStr(settings, &raw) == nil {
			if _, exists := raw["upstream_pool"]; exists {
				return true
			}
		} else if strings.Contains(settings, "\"upstream_pool\"") {
			return true
		}
	}
	if channel.BaseURL == nil {
		return false
	}
	target, err := url.Parse(strings.TrimSpace(*channel.BaseURL))
	if err != nil || target.Host == "" {
		return false
	}
	for _, name := range []string{"CLEARAPI_POOL_CLIPROXY_URL", "CLEARAPI_POOL_GROK_URL"} {
		executor, err := url.Parse(strings.TrimSpace(os.Getenv(name)))
		if err == nil && executor.Host != "" && poolAuthority(target) == poolAuthority(executor) {
			return true
		}
	}
	return false
}

func poolAuthority(endpoint *url.URL) string {
	host := strings.TrimSuffix(strings.ToLower(endpoint.Hostname()), ".")
	if host == "localhost" {
		host = "loopback"
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			host = "loopback"
		} else {
			host = ip.String()
		}
	}
	port := endpoint.Port()
	if port == "" {
		switch strings.ToLower(endpoint.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return host + ":" + port
}

// PoolRoutingEnabled reports the channel-level pool permission. It defaults to
// false and requires the channel to be pool-bound: an unbound channel claiming
// the flag is an incoherent configuration, not a permission. The flag lives in
// the channel settings JSON (pool_routing_enabled) next to the upstream_pool
// binding marker; a missing or corrupt settings blob never enables routing.
func (channel *Channel) PoolRoutingEnabled() bool {
	if channel == nil || !channel.PoolChannelBound() {
		return false
	}
	setting := ""
	if channel.Setting != nil {
		setting = *channel.Setting
	}
	for _, settings := range []string{channel.OtherSettings, setting} {
		var raw map[string]any
		if common.UnmarshalJsonStr(settings, &raw) != nil {
			continue
		}
		if flag, ok := raw["pool_routing_enabled"].(bool); ok && flag {
			return true
		}
	}
	return false
}

// UpdateChannelOtherSettingsCAS replaces a channel's settings JSON only when
// it still holds previous. The options table has no version column and the
// channel schema has no revision field, so the previous JSON blob itself is
// the compare-and-set token; callers must read back and verify (see the pool
// controller). This narrows — it cannot eliminate — the stale-write window of
// concurrent channel edits, which the admin channel UI already has.
func UpdateChannelOtherSettingsCAS(id int, previous, next string) error {
	if DB == nil {
		return ErrPoolUnavailable
	}
	result := DB.Model(&Channel{}).Where("id = ? AND settings = ?", id, previous).Update("settings", next)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := DB.Model(&Channel{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrPoolNotFound
		}
		return ErrPoolConflict
	}
	return nil
}

// CheckPoolChannelAdmission validates channel configuration coherence. Pool
// routing is an explicit operator opt-in (pool_routing_enabled in the channel
// settings); claiming it without a pool binding is rejected. Merely being
// pool-bound is allowed: request-time admission is governed by the layered
// gate, which keeps the pool closed until the operator opens every layer.
func (channel *Channel) CheckPoolChannelAdmission() error {
	var raw map[string]any
	setting := ""
	if channel.Setting != nil {
		setting = *channel.Setting
	}
	for _, settings := range []string{channel.OtherSettings, setting} {
		if common.UnmarshalJsonStr(settings, &raw) != nil {
			continue
		}
		if flag, ok := raw["pool_routing_enabled"].(bool); ok && flag && !channel.PoolChannelBound() {
			return ErrPoolRoutingRequiresBinder
		}
	}
	return nil
}
