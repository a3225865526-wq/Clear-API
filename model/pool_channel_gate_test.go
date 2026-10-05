package model

import "testing"

func TestPoolChannelBindingMarkersAndAuthority(t *testing.T) {
	t.Setenv("CLEARAPI_POOL_CLIPROXY_URL", "http://pool.invalid:8317/management")
	for _, marker := range []string{`{"upstream_pool":{}}`, `{"upstream_pool":false}`, `{"upstream_pool":null}`, `{"upstream_pool":`} {
		c := Channel{OtherSettings: marker}
		if !c.PoolChannelBound() {
			t.Fatal("marker bypass", marker)
		}
		c = Channel{Setting: &marker}
		if !c.PoolChannelBound() {
			t.Fatal("setting bypass", marker)
		}
	}
	for _, endpoint := range []string{"http://pool.invalid:8317/v1", "https://POOL.INVALID:8317/other"} {
		c := Channel{BaseURL: &endpoint}
		if !c.PoolChannelBound() {
			t.Fatal("authority bypass", endpoint)
		}
	}
	t.Setenv("CLEARAPI_POOL_CLIPROXY_URL", "http://localhost/management")
	for _, endpoint := range []string{"http://127.0.0.1:80/v1", "http://[::1]/v1", "http://LOCALHOST.:80/v1"} {
		if !(&Channel{BaseURL: &endpoint}).PoolChannelBound() {
			t.Fatal("loopback/default port bypass", endpoint)
		}
	}
	ordinary := "https://ordinary.invalid/v1"
	if (&Channel{BaseURL: &ordinary}).PoolChannelBound() {
		t.Fatal("ordinary channel detected as pool")
	}
}

// Pool routing is an explicit operator opt-in bound to a pool channel. A
// missing flag, a non-bool value, an unbound channel, and a corrupt settings
// blob all read as disabled.
func TestPoolRoutingEnabledDefaults(t *testing.T) {
	bound := `{"upstream_pool":{}}`
	cases := []struct {
		name     string
		channel  Channel
		enabled  bool
		coherent error
	}{
		{"bound-no-flag", Channel{OtherSettings: bound}, false, nil},
		{"bound-with-setting-marker", Channel{Setting: &bound}, false, nil},
		{"bound-flag-false", Channel{OtherSettings: `{"upstream_pool":{},"pool_routing_enabled":false}`}, false, nil},
		{"bound-flag-true", Channel{OtherSettings: `{"upstream_pool":{},"pool_routing_enabled":true}`}, true, nil},
		{"bound-flag-true-in-setting", Channel{Setting: &bound, OtherSettings: `{"pool_routing_enabled":true}`}, true, nil},
		{"bound-flag-non-bool", Channel{OtherSettings: `{"upstream_pool":{},"pool_routing_enabled":"yes"}`}, false, nil},
		{"unbound-flag-true", Channel{OtherSettings: `{"pool_routing_enabled":true}`}, false, ErrPoolRoutingRequiresBinder},
		{"bound-flag-true-coherent", Channel{OtherSettings: `{"upstream_pool":{},"pool_routing_enabled":true}`}, true, nil},
	}
	for _, tc := range cases {
		channel := tc.channel
		if channel.PoolRoutingEnabled() != tc.enabled {
			t.Fatalf("%s: PoolRoutingEnabled=%t", tc.name, channel.PoolRoutingEnabled())
		}
		if channel.CheckPoolChannelAdmission() != tc.coherent {
			t.Fatalf("%s: coherence=%v want %v", tc.name, channel.CheckPoolChannelAdmission(), tc.coherent)
		}
	}
	corrupt := "{"
	broken := Channel{OtherSettings: `{"upstream_pool":{`, Setting: &corrupt}
	if broken.PoolRoutingEnabled() {
		t.Fatal("corrupt settings enabled routing")
	}
	if broken.CheckPoolChannelAdmission() != nil {
		t.Fatal("corrupt settings rejected a merely bound channel")
	}
}
