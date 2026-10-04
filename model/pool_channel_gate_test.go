package model

import "testing"

func TestPoolChannelAdmissionMarkersAndAuthority(t *testing.T) {
	t.Setenv("CLEARAPI_POOL_CLIPROXY_URL", "http://pool.invalid:8317/management")
	for _, marker := range []string{`{"upstream_pool":{}}`, `{"upstream_pool":false}`, `{"upstream_pool":null}`, `{"upstream_pool":`} {
		c := Channel{OtherSettings: marker}
		if c.CheckPoolChannelAdmission() != ErrPoolChannelAdmission {
			t.Fatal("marker bypass", marker)
		}
		c = Channel{Setting: &marker}
		if c.CheckPoolChannelAdmission() != ErrPoolChannelAdmission {
			t.Fatal("setting bypass", marker)
		}
	}
	for _, endpoint := range []string{"http://pool.invalid:8317/v1", "https://POOL.INVALID:8317/other"} {
		c := Channel{BaseURL: &endpoint}
		if c.CheckPoolChannelAdmission() != ErrPoolChannelAdmission {
			t.Fatal("authority bypass", endpoint)
		}
	}
	t.Setenv("CLEARAPI_POOL_CLIPROXY_URL", "http://localhost/management")
	for _, endpoint := range []string{"http://127.0.0.1:80/v1", "http://[::1]/v1", "http://LOCALHOST.:80/v1"} {
		if (&Channel{BaseURL: &endpoint}).CheckPoolChannelAdmission() != ErrPoolChannelAdmission {
			t.Fatal("loopback/default port bypass", endpoint)
		}
	}
	ordinary := "https://ordinary.invalid/v1"
	if (&Channel{BaseURL: &ordinary}).CheckPoolChannelAdmission() != nil {
		t.Fatal("ordinary channel changed")
	}
}
