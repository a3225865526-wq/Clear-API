package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var errConsoleProbeTransport = errors.New("MONITOR_PROBE_TRANSPORT_FAILED")

// The unauthenticated loopback form remains available to transport unit tests.
func sendConsoleProbe(ctx context.Context, m model.ConsoleMonitor, policy consoleProbeCostPolicy, endpoint string, timeout time.Duration) ([]byte, error) {
	return sendConsoleProbeHTTP(ctx, m, policy, endpoint, timeout, "", "")
}

func sendConsoleProbeHTTP(ctx context.Context, m model.ConsoleMonitor, policy consoleProbeCostPolicy, endpoint string, timeout time.Duration, key, requestID string) ([]byte, error) {
	if _, err := policy.WorstCaseCost(m, consoleProbePayload, time.Now()); err != nil {
		return nil, err
	}
	u, err := url.Parse(endpoint)
	loopback := err == nil && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
	if err != nil || !(u.Scheme == "https" && key != "" && endpoint == policy.endpoint || u.Scheme == "http" && loopback) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/responses" ||
		u.RawPath != "" || timeout <= 0 || timeout > 120*time.Second {
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
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-Client-Request-Id", requestID)
	}
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

// ProbeConsoleChannel uses the exact frozen payload and a single authenticated
// HTTP/1 request. It never invokes user relay, fallback routing or settlement.
func ProbeConsoleChannel(ctx context.Context, db *gorm.DB, m model.ConsoleMonitor, attemptID int64) (ConsoleProbeOutcome, error) {
	contract, err := LoadConsoleProbeCostContract(ctx, db, m)
	if err != nil {
		return ConsoleProbeOutcome{}, err
	}
	policy, err := consoleProbeContractPolicy(ctx, db, m, contract)
	if err != nil {
		return ConsoleProbeOutcome{}, err
	}
	body, err := sendConsoleProbeHTTP(ctx, m, policy, policy.endpoint, time.Duration(m.TimeoutSeconds)*time.Second, policy.key, fmt.Sprintf("clearapi-monitor-%d-%d", m.ID, attemptID))
	if err != nil {
		return ConsoleProbeOutcome{}, errors.Join(err, ErrMonitorLeaseUnresolved)
	}
	var receipt struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Model  string `json:"model"`
		Error  any    `json:"error"`
		Usage  *struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if common.Unmarshal(body, &receipt) != nil || receipt.ID == "" || len(receipt.ID) > 128 || (receipt.Status != "completed" && receipt.Status != "incomplete") || receipt.Model != m.Model || receipt.Error != nil || receipt.Usage == nil || receipt.Usage.Input == nil || receipt.Usage.Output == nil || *receipt.Usage.Input < 0 || *receipt.Usage.Output < 0 || *receipt.Usage.Input > policy.InputTokenLimit || *receipt.Usage.Output > policy.OutputTokenLimit {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	cost := *receipt.Usage.Input*policy.proof.InputMicros + *receipt.Usage.Output*policy.proof.OutputMicros + policy.proof.FixedFeeMicros
	if cost > policy.HardCeilingMicros || cost > m.RequestCostMicros {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorCostExceeded, ErrMonitorLeaseUnresolved)
	}
	status, code := "normal", ""
	if receipt.Status == "incomplete" {
		status, code = "degraded", "probe_incomplete"
	}
	return ConsoleProbeOutcome{Status: status, ErrorCode: code, SourceChannelID: m.ChannelID, MeteredCostMicros: &cost, UpstreamResponseID: receipt.ID}, nil
}
