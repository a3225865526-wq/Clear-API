package zhipu

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The legacy Zhipu v3 surface is a proprietary prompt format. A Claude request
// used to hit an unconditional panic (a 500 with no usable message); it must be
// a client error that skips channel retries instead.
func TestConvertClaudeRequestRejectsWithoutPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: 4, UpstreamModelName: "chatglm_turbo"}}

	require.NotPanics(t, func() {
		_, err := (&Adaptor{}).ConvertClaudeRequest(c, info, &dto.ClaudeRequest{Model: "chatglm_turbo"})
		var apiErr *types.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.Contains(t, apiErr.Error(), "/v1/messages")
	})
}

func TestConvertGeminiRequestRejectsLegacySurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/chatglm_turbo:generateContent", nil)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: 4, UpstreamModelName: "chatglm_turbo"}}

	_, err := (&Adaptor{}).ConvertGeminiRequest(c, info, &dto.GeminiChatRequest{})
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
}

// streamWriter wraps gin's own response writer (so every interface method stays
// satisfied) and supplies the CloseNotify side of gin's Context.Stream, which
// the plain httptest recorder behind a test context does not provide.
type streamWriter struct {
	gin.ResponseWriter
	gone chan bool
}

func newStreamWriter(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/paas/v3/model-api/chatglm_turbo/sse-invoke", nil)
	c.Writer = &streamWriter{ResponseWriter: c.Writer, gone: make(chan bool, 1)}
	return c, recorder
}

func (w *streamWriter) CloseNotify() <-chan bool { return w.gone }

// disconnectingWriter cancels the request context once the payload containing
// needle has been written, simulating a client that disconnects mid-stream.
type disconnectingWriter struct {
	*streamWriter
	needle string
	cancel context.CancelFunc
	once   sync.Once
}

func (w *disconnectingWriter) Write(p []byte) (int, error) {
	n, err := w.streamWriter.Write(p)
	if strings.Contains(string(p), w.needle) {
		w.once.Do(w.cancel)
	}
	return n, err
}

func (w *disconnectingWriter) WriteString(s string) (int, error) {
	n, err := io.WriteString(w.streamWriter, s)
	if strings.Contains(s, w.needle) {
		w.once.Do(w.cancel)
	}
	return n, err
}

// trackedBody records whether the relay closed the upstream body, which is what
// releases a reader parked on a scanner.
type trackedBody struct {
	*strings.Reader
	mu     sync.Mutex
	closed bool
}

func (b *trackedBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

func (b *trackedBody) wasClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func newZhipuStreamInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "chatglm_turbo"},
		IsStream:    true,
	}
}

// sseEvents splits a rendered body into event payloads, dropping the blank
// separators between frames.
func sseEvents(body string) []string {
	var events []string
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		events = append(events, strings.TrimPrefix(frame, "data: "))
	}
	return events
}

// A v3 SSE body arrives as one burst far more often than not (buffered proxies,
// loopback upstreams). Every frame that was read, plus the meta frame carrying
// usage, must be delivered before [DONE] and in body order. Separate buffered
// data/meta/stop channels let the completion signal win the select and silently
// drop the tail.
func TestZhipuStreamHandlerBurstKeepsEveryFrameAndUsage(t *testing.T) {
	const frames = 40
	var body strings.Builder
	for i := range frames {
		fmt.Fprintf(&body, "data:TOKEN%02d\n", i)
	}
	body.WriteString(`meta:{"request_id":"req-1","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n")

	for run := range 50 {
		c, recorder := newStreamWriter(t)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body.String())),
			Header:     http.Header{},
		}

		usage, apiErr := zhipuStreamHandler(c, newZhipuStreamInfo(), resp)
		require.Nil(t, apiErr, "run %d", run)
		require.NotNil(t, usage, "run %d lost the usage frame", run)
		require.Equal(t, 10, usage.PromptTokens, "run %d", run)
		require.Equal(t, 5, usage.CompletionTokens, "run %d", run)
		require.Equal(t, 15, usage.TotalTokens, "run %d", run)

		events := sseEvents(recorder.Body.String())
		require.NotEmpty(t, events, "run %d", run)
		require.Equal(t, "[DONE]", events[len(events)-1], "run %d must end with the terminal marker", run)

		delivered := 0
		finishSeen := false
		for _, event := range events[:len(events)-1] {
			if strings.Contains(event, `"finish_reason":"stop"`) {
				finishSeen = true
				continue
			}
			require.False(t, finishSeen, "run %d: chunk delivered after the meta frame", run)
			require.Equal(t, fmt.Sprintf("TOKEN%02d", delivered), chunkContent(t, event), "run %d: chunk order", run)
			delivered++
		}
		require.Equal(t, frames, delivered, "run %d dropped burst chunks", run)
		require.True(t, finishSeen, "run %d dropped the meta frame", run)
	}
}

// chunkContent extracts the delta content of one converted chunk.
func chunkContent(t *testing.T, event string) string {
	t.Helper()
	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(event, &chunk))
	require.Len(t, chunk.Choices, 1)
	return chunk.Choices[0].Delta.GetContentString()
}

// A client that disconnects mid-stream must stop the handler promptly and must
// not leave the reader goroutine parked on an unread channel.
func TestZhipuStreamHandlerExitsOnClientDisconnect(t *testing.T) {
	const frames = 200
	var body strings.Builder
	for i := range frames {
		fmt.Fprintf(&body, "data:TOKEN%03d\n", i)
	}
	body.WriteString(`meta:{"request_id":"req-1","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n")

	c, recorder := newStreamWriter(t)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	// Context.Stream reads CloseNotify from c.Writer; disconnectingWriter embeds
	// *streamWriter so the wrapper keeps that method.
	c.Writer = &disconnectingWriter{
		streamWriter: c.Writer.(*streamWriter),
		needle:       "TOKEN000",
		cancel:       cancel,
	}

	upstream := &trackedBody{Reader: strings.NewReader(body.String())}
	resp := &http.Response{StatusCode: http.StatusOK, Body: upstream, Header: http.Header{}}

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = zhipuStreamHandler(c, newZhipuStreamInfo(), resp)
	}()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("stream handler did not exit after the client disconnected")
	}

	require.True(t, upstream.wasClosed(), "upstream body must be closed to release the reader")
	require.NotContains(t, recorder.Body.String(), fmt.Sprintf("TOKEN%03d", frames-1),
		"the handler must stop forwarding after the disconnect")
	require.NotContains(t, recorder.Body.String(), "[DONE]",
		"a cancelled stream must not be closed as if it completed")
}

// A handler whose upstream ends between frames still terminates the client
// stream with [DONE] once the body is drained.
func TestZhipuStreamHandlerEmitsDoneAfterEmptyBody(t *testing.T) {
	c, recorder := newStreamWriter(t)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
	}

	usage, apiErr := zhipuStreamHandler(c, newZhipuStreamInfo(), resp)
	require.Nil(t, apiErr)
	require.Nil(t, usage, "no meta frame means no usage")
	require.Equal(t, []string{"[DONE]"}, sseEvents(recorder.Body.String()))
}
