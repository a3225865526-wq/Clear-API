package model

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var ErrPoolChannelAdmission = errors.New("POOL_CHANNEL_DISABLED: continuous account isolation and exact metering admission are not verified")

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

// CheckPoolChannelAdmission deliberately has no configuration override. An
// owner role, available account, or DEDICATED flag is not account isolation.
func (channel *Channel) CheckPoolChannelAdmission() error {
	if channel.PoolChannelBound() {
		return ErrPoolChannelAdmission
	}
	return nil
}
