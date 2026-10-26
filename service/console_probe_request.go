package service

import (
	"encoding/base64"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/tidwall/gjson"
)

// Native requests are operator-approved samples, not client passthrough. Their
// exact bytes, endpoint, model and protocol are bound to the supplier contract.
// This also covers multipart audio, binary inputs and asynchronous media APIs.
type ConsoleProbeNativeRequest struct {
	ResponseContentType string                  `json:"response_content_type"`
	SuccessPath         string                  `json:"success_path,omitempty"`
	SuccessValues       []string                `json:"success_values,omitempty"`
	Path                string                  `json:"path"`
	ContentType         string                  `json:"content_type"`
	BodyBase64          string                  `json:"body_base64"`
	KeyHeader           string                  `json:"key_header,omitempty"`
	Poll                *ConsoleProbeNativePoll `json:"poll,omitempty"`
}

type ConsoleProbeNativePoll struct {
	Path            string   `json:"path"` // relative path containing {id}
	IDPath          string   `json:"id_path"`
	StatusPath      string   `json:"status_path"`
	CompletedStates []string `json:"completed_states"`
	FailedStates    []string `json:"failed_states"`
}

func consoleProbeRequest(m model.ConsoleMonitor, native *ConsoleProbeNativeRequest) (string, string, string, string, error) {
	if native != nil {
		if len(native.BodyBase64) > base64.StdEncoding.EncodedLen(1<<20) {
			return "", "", "", "", model.ErrMonitorInvalid
		}
		path, err := url.Parse(native.Path)
		body, decodeErr := base64.StdEncoding.DecodeString(native.BodyBase64)
		if err != nil || path.IsAbs() || path.Host != "" || !strings.HasPrefix(native.Path, "/") || strings.HasPrefix(native.Path, "//") || path.Fragment != "" ||
			decodeErr != nil || len(body) == 0 || len(body) > 1<<20 || native.ContentType == "" || strings.ContainsAny(native.ContentType, "\r\n") || native.ResponseContentType == "" || (strings.Contains(native.ResponseContentType, "json") && native.Poll == nil && native.SuccessPath == "") {
			return "", "", "", "", model.ErrMonitorInvalid
		}
		keyHeader := native.KeyHeader
		if strings.HasPrefix(native.ContentType, "application/json") {
			var request any
			if common.Unmarshal(body, &request) != nil {
				return "", "", "", "", model.ErrMonitorInvalid
			}
			if target := gjson.GetBytes(body, "model"); target.Type == gjson.String && target.String() != m.Model {
				return "", "", "", "", model.ErrMonitorInvalid
			}
			for _, field := range []struct {
				name  string
				limit float64
			}{{"n", dto.MaxImageN}, {"seconds", relaycommon.MaxTaskDurationSeconds}, {"duration", relaycommon.MaxTaskDurationSeconds}} {
				quantity := gjson.GetBytes(body, field.name)
				if !quantity.Exists() || quantity.Type == gjson.Null {
					continue
				}
				value, parseErr := strconv.ParseFloat(strings.TrimSuffix(quantity.String(), "s"), 64)
				if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > field.limit || field.name == "n" && math.Trunc(value) != value {
					return "", "", "", "", model.ErrMonitorInvalid
				}
			}
		}
		if keyHeader == "" {
			keyHeader = "Authorization"
		}
		// A credential header cannot replace routing/framing headers.
		if strings.IndexFunc(keyHeader, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
		}) >= 0 ||
			strings.EqualFold(keyHeader, "Host") || strings.EqualFold(keyHeader, "Content-Type") || strings.EqualFold(keyHeader, "Content-Length") || strings.EqualFold(keyHeader, "Connection") || strings.EqualFold(keyHeader, "Transfer-Encoding") || strings.EqualFold(keyHeader, "Cookie") {
			return "", "", "", "", model.ErrMonitorInvalid
		}
		if poll := native.Poll; poll != nil {
			p, parseErr := url.Parse(poll.Path)
			if parseErr != nil || p.IsAbs() || p.Host != "" || !strings.HasPrefix(poll.Path, "/") || strings.HasPrefix(poll.Path, "//") || p.Fragment != "" ||
				!strings.Contains(poll.Path, "{id}") || poll.IDPath == "" || poll.StatusPath == "" || len(poll.CompletedStates) == 0 || len(poll.IDPath) > 128 || len(poll.StatusPath) > 128 {
				return "", "", "", "", model.ErrMonitorInvalid
			}
		}
		return native.Path, string(body), native.ContentType, http.CanonicalHeaderKey(keyHeader), nil
	}
	var path, keyHeader string
	var body any
	switch m.Protocol {
	case "openai_chat", "openai":
		path, keyHeader = "/v1/chat/completions", "Authorization"
		body = map[string]any{"model": m.Model, "messages": []any{map[string]any{"role": "user", "content": "monitor probe"}}, "stream": false, "max_completion_tokens": 16}
	case "openai_responses", "openai-response":
		path, keyHeader = "/v1/responses", "Authorization"
		body = map[string]any{"model": m.Model, "input": "monitor probe", "stream": false, "max_output_tokens": 16, "store": false}
	case "anthropic", "claude":
		path, keyHeader = "/v1/messages", "X-Api-Key"
		body = map[string]any{"model": m.Model, "messages": []any{map[string]any{"role": "user", "content": "monitor probe"}}, "max_tokens": 16, "stream": false}
	case "gemini":
		path, keyHeader = "/v1beta/models/"+url.PathEscape(m.Model)+":generateContent", "X-Goog-Api-Key"
		body = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "monitor probe"}}}}, "generationConfig": map[string]any{"maxOutputTokens": 16}}
	default:
		// No protocol allowlist: other native formats provide their exact sample
		// through the same contract, including image/audio/video and future APIs.
		return "", "", "", "", ErrMonitorCostUnverified
	}
	encoded, err := common.Marshal(body)
	return path, string(encoded), "application/json", keyHeader, err
}
