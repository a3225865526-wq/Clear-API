package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
)

var errConsoleProbeTransport = errors.New("MONITOR_PROBE_TRANSPORT_FAILED")

// Validation-only transport: no channel credentials, production endpoint,
// registration, shared client, user relay, finance or billing integration.
// Literal loopback HTTP is the ONLY destination supported in this phase.
func sendConsoleProbe(ctx context.Context, m model.ConsoleMonitor, policy consoleProbeCostPolicy, endpoint string, timeout time.Duration) ([]byte, error) {
	if _, err := policy.WorstCaseCost(m, consoleProbePayload, time.Now()); err != nil {
		return nil, err
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/responses" ||
		u.RawPath != "" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || timeout <= 0 || timeout > 120*time.Second {
		return nil, ErrMonitorCostUnverified
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// One fresh HTTP/1 connection, no proxy, no connection reuse, no GetBody and
	// no retry loop. In particular, this does not inherit HTTP/2 stream retries.
	transport := &http.Transport{DisableKeepAlives: true, ForceAttemptHTTP2: false,
		DialContext:            (&net.Dialer{}).DialContext,
		MaxResponseHeaderBytes: 16 << 10,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, io.NopCloser(strings.NewReader(consoleProbePayload)))
	if err != nil {
		return nil, errConsoleProbeTransport
	}
	req.ContentLength = int64(len(consoleProbePayload))
	req.Header.Set("Content-Type", "application/json")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		// A closed socket is NOT proof of provider cancellation. Preserve the
		// executor lease if dispatch/remote completion cannot be established.
		return nil, errors.Join(errConsoleProbeTransport, ctx.Err(), ErrMonitorLeaseUnresolved)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errConsoleProbeTransport
	}
	const responseLimit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(body) > responseLimit {
		return nil, errors.Join(errConsoleProbeTransport, ctx.Err(), ErrMonitorLeaseUnresolved)
	}
	return body, nil
}
