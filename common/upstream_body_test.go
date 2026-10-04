package common

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type observedUpstreamBody struct {
	io.Reader
	closed bool
}

func (b *observedUpstreamBody) Close() error { b.closed = true; return nil }

func TestBoundedUpstreamBody(t *testing.T) {
	t.Setenv("MAX_UPSTREAM_RESPONSE_MB", "1")
	for _, size := range []int{3, 1 << 20, (1 << 20) + 1} {
		body := &observedUpstreamBody{Reader: strings.NewReader(strings.Repeat("x", size))}
		data, err := ReadUpstreamBody(body)
		require.True(t, body.closed)
		if size > 1<<20 {
			require.ErrorIs(t, err, ErrUpstreamBodyTooLarge)
			require.Nil(t, data)
		} else {
			require.NoError(t, err)
			require.Len(t, data, size)
		}
	}
	t.Setenv("MAX_UPSTREAM_RESPONSE_MB", "0")
	body := &observedUpstreamBody{Reader: strings.NewReader("x")}
	_, err := ReadUpstreamBody(body)
	require.Error(t, err)
	require.True(t, body.closed)
}
