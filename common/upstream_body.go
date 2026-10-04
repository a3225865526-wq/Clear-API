package common

import (
	"errors"
	"io"
)

var ErrUpstreamBodyTooLarge = errors.New("upstream response exceeds configured size limit")

// A body read failure after dispatch does not prove that upstream did no work.
// Preserve this fact independently of provider-specific error wrapping.
func ReadUpstreamBodyForRequest(ctx interface{ Set(string, any) }, body io.ReadCloser) ([]byte, error) {
	data, err := ReadUpstreamBody(body)
	if err != nil && ctx != nil {
		ctx.Set("upstream_usage_uncertain", true)
		ctx.Set("upstream_body_read_failed", true)
	}
	return data, err
}

// ReadUpstreamBody bounds buffered responses independently of client input.
// Closing on every path releases the connection and cancels further reads.
func ReadUpstreamBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	mb := GetEnvOrDefault("MAX_UPSTREAM_RESPONSE_MB", 16)
	if mb < 1 || mb > 128 {
		return nil, errors.New("invalid upstream response size limit")
	}
	limit := int64(mb) * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrUpstreamBodyTooLarge
	}
	return data, nil
}
