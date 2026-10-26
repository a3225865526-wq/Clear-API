package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

var errConsoleProbeTransport = errors.New("MONITOR_PROBE_TRANSPORT_FAILED")

// The unauthenticated loopback form remains available to transport unit tests.
func sendConsoleProbe(ctx context.Context, m model.ConsoleMonitor, policy consoleProbeCostPolicy, endpoint string, timeout time.Duration) ([]byte, error) {
	return sendConsoleProbeHTTP(ctx, m, policy, endpoint, timeout, "", "")
}

func sendConsoleProbeHTTP(ctx context.Context, m model.ConsoleMonitor, policy consoleProbeCostPolicy, endpoint string, timeout time.Duration, key, requestID string) ([]byte, error) {
	return sendConsoleProbeNativeHTTP(ctx, m, policy, http.MethodPost, endpoint, policy.payload, timeout, key, requestID)
}

func sendConsoleProbeNativeHTTP(ctx context.Context, m model.ConsoleMonitor, policy consoleProbeCostPolicy, method, endpoint, payload string, timeout time.Duration, key, requestID string) ([]byte, error) {
	if _, err := policy.WorstCaseCost(m, policy.payload, time.Now()); err != nil {
		return nil, err
	}
	u, err := url.Parse(endpoint)
	loopback := err == nil && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()
	if err != nil || !(u.Scheme == "https" && key != "" && (endpoint == policy.endpoint || method == http.MethodGet && policy.native != nil && policy.native.Poll != nil && strings.HasPrefix(endpoint, strings.TrimSuffix(policy.endpoint, policy.native.Path)+"/")) || u.Scheme == "http" && loopback) || u.User != nil || u.Fragment != "" ||
		timeout <= 0 || timeout > 120*time.Second {
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
	req, err := http.NewRequestWithContext(ctx, method, endpoint, io.NopCloser(strings.NewReader(payload)))
	if err != nil {
		return nil, errConsoleProbeTransport
	}
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", policy.contentType)
	if m.Protocol == "anthropic" || m.Protocol == "claude" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if key != "" {
		credential := key
		if policy.keyHeader == "Authorization" {
			credential = "Bearer " + key
		}
		req.Header.Set(policy.keyHeader, credential)
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
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if contentType == "text/html" || (policy.native != nil && method == http.MethodPost && contentType != policy.native.ResponseContentType) {
		return nil, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	const responseLimit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(body) > responseLimit {
		return nil, errors.Join(errConsoleProbeTransport, ctx.Err(), ErrMonitorLeaseUnresolved)
	}
	return body, nil
}

// ProbeConsoleChannel uses the exact contract-bound payload and an authenticated
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
	if policy.native != nil {
		if poll := policy.native.Poll; poll != nil {
			id := gjson.GetBytes(body, poll.IDPath).String()
			if id == "" || len(id) > 128 || strings.ContainsAny(id, " /?&#%\\\r\n") {
				return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
			}
			pollEndpoint := strings.TrimSuffix(policy.endpoint, policy.native.Path) + strings.ReplaceAll(poll.Path, "{id}", url.PathEscape(id))
			pollCtx, cancel := context.WithTimeout(ctx, time.Duration(m.TimeoutSeconds)*time.Second)
			defer cancel()
			for {
				body, err = sendConsoleProbeNativeHTTP(pollCtx, m, policy, http.MethodGet, pollEndpoint, "", time.Duration(m.TimeoutSeconds)*time.Second, policy.key, fmt.Sprintf("clearapi-monitor-%d-%d", m.ID, attemptID))
				if err != nil {
					return ConsoleProbeOutcome{}, errors.Join(err, ErrMonitorLeaseUnresolved)
				}
				state := gjson.GetBytes(body, poll.StatusPath).String()
				if slices.Contains(poll.CompletedStates, state) {
					break
				}
				if slices.Contains(poll.FailedStates, state) {
					// A terminal failure confirms the job stopped. Retain the full
					// reserved budget and report no invented actual charge.
					return ConsoleProbeOutcome{Status: "error", ErrorCode: "probe_failed", SourceChannelID: m.ChannelID, UpstreamResponseID: id, completionConfirmed: true}, nil
				}
				timer := time.NewTimer(time.Second)
				select {
				case <-pollCtx.Done():
					timer.Stop()
					return ConsoleProbeOutcome{}, errors.Join(pollCtx.Err(), ErrMonitorLeaseUnresolved)
				case <-timer.C:
				}
			}
			cost := policy.proof.FixedFeeMicros
			return ConsoleProbeOutcome{Status: "normal", SourceChannelID: m.ChannelID, MeteredCostMicros: &cost, UpstreamResponseID: id}, nil
		}
		// Native flat-request contracts attest the complete successful request fee,
		// including image/audio and other non-token charges. A maximum estimate is
		// not an exact charge and must not be configured as a flat-request contract.
		if len(body) == 0 || gjson.GetBytes(body, "error").Exists() && gjson.GetBytes(body, "error").Type != gjson.Null || gjson.GetBytes(body, "success").Type == gjson.False {
			return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
		}
		if policy.native.SuccessPath != "" {
			result := gjson.GetBytes(body, policy.native.SuccessPath)
			if !result.Exists() || result.String() == "" || len(policy.native.SuccessValues) > 0 && !slices.Contains(policy.native.SuccessValues, result.String()) {
				return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
			}
		}
		cost := policy.proof.FixedFeeMicros
		return ConsoleProbeOutcome{Status: "normal", SourceChannelID: m.ChannelID, MeteredCostMicros: &cost, UpstreamResponseID: gjson.GetBytes(body, "id").String(), completionConfirmed: true}, nil
	}
	var receipt struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Model  string `json:"model"`
		Error  any    `json:"error"`
		Usage  *struct {
			Input      *int64 `json:"input_tokens"`
			Output     *int64 `json:"output_tokens"`
			Prompt     *int64 `json:"prompt_tokens"`
			Completion *int64 `json:"completion_tokens"`
		} `json:"usage"`
		UsageMetadata *struct {
			Input  *int64 `json:"promptTokenCount"`
			Output *int64 `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if common.Unmarshal(body, &receipt) != nil || receipt.Error != nil {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	var input, output *int64
	if receipt.Usage != nil {
		input, output = receipt.Usage.Input, receipt.Usage.Output
		if input == nil {
			input = receipt.Usage.Prompt
		}
		if output == nil {
			output = receipt.Usage.Completion
		}
	}
	if receipt.UsageMetadata != nil {
		input, output = receipt.UsageMetadata.Input, receipt.UsageMetadata.Output
	}
	if input == nil || output == nil || *input < 0 || *output < 0 || *input > policy.InputTokenLimit || *output > policy.OutputTokenLimit || (receipt.Model != "" && receipt.Model != m.Model) {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	// Reuse protocol billing semantics so Claude cache input and Gemini
	// reasoning output are not omitted from the supplier cost observation.
	var normalized *dto.BillingUsage
	if m.Protocol == "anthropic" || m.Protocol == "claude" {
		var native struct {
			Usage dto.ClaudeUsage `json:"usage"`
		}
		if common.Unmarshal(body, &native) != nil {
			return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
		}
		values := []int{native.Usage.InputTokens, native.Usage.CacheReadInputTokens, native.Usage.CacheCreationInputTokens, native.Usage.GetCacheCreation5mTokens(), native.Usage.GetCacheCreation1hTokens()}
		for _, value := range values {
			if value < 0 || int64(value) > policy.InputTokenLimit {
				return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
			}
		}
		normalized = dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: native.Usage.InputTokens, CacheReadInputTokens: native.Usage.CacheReadInputTokens, CacheCreationInputTokens: native.Usage.GetCacheCreationTotalTokens(), OutputTokens: native.Usage.OutputTokens})
	}
	if receipt.UsageMetadata != nil {
		var native struct {
			Usage dto.GeminiUsageMetadata `json:"usageMetadata"`
		}
		if common.Unmarshal(body, &native) != nil {
			return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
		}
		for _, value := range []int{native.Usage.PromptTokenCount, native.Usage.ToolUsePromptTokenCount, native.Usage.CachedContentTokenCount} {
			if value < 0 || int64(value) > policy.InputTokenLimit {
				return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
			}
		}
		if native.Usage.ThoughtsTokenCount < 0 || int64(native.Usage.ThoughtsTokenCount) > policy.OutputTokenLimit {
			return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
		}
		normalized = dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: native.Usage.PromptTokenCount, ToolUsePromptTokenCount: native.Usage.ToolUsePromptTokenCount, CandidatesTokenCount: native.Usage.CandidatesTokenCount, ThoughtsTokenCount: native.Usage.ThoughtsTokenCount, CachedContentTokenCount: native.Usage.CachedContentTokenCount})
	}
	if normalized != nil {
		usage, _ := normalized.CanonicalUsage()
		inputTokens, outputTokens := int64(usage.InputTokens), int64(usage.OutputTokens)
		if normalized.Semantic == dto.BillingUsageSemanticGemini {
			inputTokens, outputTokens = int64(usage.PromptTokens), int64(usage.CompletionTokens)
		}
		if inputTokens > policy.InputTokenLimit || outputTokens > policy.OutputTokenLimit {
			return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
		}
		input, output = &inputTokens, &outputTokens
	}
	if receipt.ID == "" && (m.Protocol == "openai_responses" || m.Protocol == "openai-response") {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	if len(receipt.ID) > 128 {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	if receipt.Status != "" && receipt.Status != "completed" && receipt.Status != "incomplete" {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorMetering, ErrMonitorLeaseUnresolved)
	}
	cost := *input*policy.proof.InputMicros + *output*policy.proof.OutputMicros + policy.proof.FixedFeeMicros
	if cost > policy.HardCeilingMicros || cost > m.RequestCostMicros {
		return ConsoleProbeOutcome{}, errors.Join(ErrMonitorCostExceeded, ErrMonitorLeaseUnresolved)
	}
	status, code := "normal", ""
	if receipt.Status == "incomplete" {
		status, code = "degraded", "probe_incomplete"
	}
	return ConsoleProbeOutcome{Status: status, ErrorCode: code, SourceChannelID: m.ChannelID, MeteredCostMicros: &cost, UpstreamResponseID: receipt.ID, completionConfirmed: true}, nil
}
