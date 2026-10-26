package service_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type r7CountBody struct {
	io.ReadCloser
	n *atomic.Int64
}

func (b r7CountBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n.Add(int64(n))
	return n, err
}

type r7CountWriter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (w r7CountWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.n.Add(int64(n))
	return n, err
}

func (w r7CountWriter) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

// No external networking: run with Docker --network none. HTTP headers are
// control traffic; the byte counters measure response BODY/output, not headers.
// The mock records billable input processing before cancellation, but supplies
// no usage to the proxy. This distinguishes zero output from proven zero cost.
func TestR7ZeroChunkClosure(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)

	u := createHardeningUser(t, model.DB, "r7-zero-closure", 10000)
	f := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	f.Info.IsStream = true
	f.Info.DisablePing = true
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	var generated, sent, received, forwarded, inputProcessed atomic.Int64
	mockReady, mockCanceled, proxyStreaming, proxyCanceled := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		inputProcessed.Store(100) // Mock ground truth; deliberately NOT injected into settlement.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush() // Headers only: permits testing the real stream scanner.
		close(mockReady)
		// Generation and all body writes remain blocked until cancellation.
		<-r.Context().Done()
		close(mockCanceled)
	}))
	defer mock.Close()
	result := make(chan error, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(r7CountWriter{w, &forwarded})
		c.Request = r
		c.Set(common.RequestIdKey, f.RequestID)
		f.Ctx = c
		go func() { <-r.Context().Done(); close(proxyCanceled) }()
		req, err := http.NewRequest(http.MethodPost, mock.URL, strings.NewReader(f.OutboundBody))
		if err != nil {
			result <- err
			return
		}
		resp, err := channel.DoRequest(c, req, f.Info) // Actual production dispatch/context propagation.
		if err != nil {
			result <- err
			return
		}
		resp.Body = r7CountBody{resp.Body, &received}
		close(proxyStreaming)
		usage, apiErr := openai.OaiStreamHandler(c, f.Info, resp)
		if apiErr != nil {
			result <- apiErr
			return
		}
		service.PostTextConsumeQuota(c, f.Info, usage, nil)
		result <- nil
	}))
	defer proxy.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, proxy.URL, strings.NewReader(f.OutboundBody))
		resp, _ := proxy.Client().Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()
	await := func(ch <-chan struct{}, name string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout: %s", name)
		}
	}
	await(mockReady, "mock accepted")
	await(proxyStreaming, "proxy entered stream")
	reserved := reservationOf(t, f.RequestID).Reserved
	require.Positive(t, reserved)
	var cancellations sync.WaitGroup
	for range 2 {
		cancellations.Go(cancel) // Concurrent, repeated cancellation of the real request.
	}
	cancellations.Wait()
	await(proxyCanceled, "cancel reached proxy")
	await(mockCanceled, "cancel reached mock")
	await(clientDone, "client cancellation")
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("settlement timeout")
	}
	row := reservationOf(t, f.RequestID)
	wallet := walletOf(t, u)
	var markers int64
	require.NoError(t, model.DB.Model(&model.BillingReviewMarker{}).Where("reservation_id = ?", row.ID).Count(&markers).Error)
	obs := service.TokenObservationForLog(f.Ctx, false)
	t.Logf("CONTROLLED_FAULT evidence: input_processed=%d generated_output=%d mock_body_sent=%d proxy_body_received=%d proxy_body_forwarded=%d cancel_proxy=true cancel_mock=true repeated_cancel=true headers_sent=true", inputProcessed.Load(), generated.Load(), sent.Load(), received.Load(), forwarded.Load())
	t.Logf("billing: reserved=%d target=%d applied=%d state=%s wallet_before=%d wallet_after=%d token_after=%d markers=%d quality=%s interrupted=%v", reserved, row.Target, row.Applied, row.State, u.Balance, wallet.UserQuota, wallet.TokenRemain, markers, obs.Quality, obs.Interrupted)
	require.Zero(t, generated.Load())
	require.Zero(t, sent.Load())
	require.Zero(t, received.Load())
	require.Zero(t, forwarded.Load())
	require.True(t, obs.Interrupted)
	require.True(t, f.Ctx.GetBool("upstream_attempted"))
	require.Equal(t, "missing", obs.Quality)
	require.EqualValues(t, 100, inputProcessed.Load())
	require.GreaterOrEqual(t, row.Applied, 0)
	require.LessOrEqual(t, row.Applied, reserved)
	require.Equal(t, u.Balance-row.Applied, wallet.UserQuota)
	require.Equal(t, wallet.UserQuota, wallet.TokenRemain)
	// Unknown billable input must not silently become a proven zero bill.
	require.Equal(t, reserved, row.Applied, "zero output does not prove zero provider cost: retain unknown usage for review")
	require.EqualValues(t, 1, markers, "unknown usage must remain auditable")
	marker, ok := markerOf(t, f.RequestID)
	require.True(t, ok)
	require.Equal(t, "interrupted_unknown_usage", marker.Reason)
	require.Equal(t, reserved, row.Target)
	// Replay settlement and refunds after the retained completion. Only the
	// billing API is concurrent: PostTextConsumeQuota also mutates log state.
	service.PostTextConsumeQuota(f.Ctx, f.Info, &dto.Usage{}, nil)
	session := f.Info.Billing.(*service.BillingSession)
	results := make(chan error, 2)
	var completions sync.WaitGroup
	for range 2 {
		completions.Go(func() {
			results <- session.Settle(reserved)
			session.Refund(f.Ctx)
		})
	}
	completions.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, row, reservationOf(t, f.RequestID))
	require.Equal(t, wallet, walletOf(t, u))
	require.NoError(t, model.DB.Model(&model.BillingReviewMarker{}).Where("reservation_id = ?", row.ID).Count(&markers).Error)
	require.EqualValues(t, 1, markers)
}

func TestR7CancelBeforeTransportWrite(t *testing.T) {
	newHardeningDB(t)
	configureTieredModel(t, h2ModelA, fixtureAExpr)

	u := createHardeningUser(t, model.DB, "r7-before-write", 10000)
	f := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
	var accepted atomic.Int64
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { accepted.Add(1) }))
	defer mock.Close()
	ctx, cancel := context.WithCancel(f.Ctx.Request.Context())
	cancel()
	f.Ctx.Request = f.Ctx.Request.WithContext(ctx)
	req, err := http.NewRequest(http.MethodPost, mock.URL, strings.NewReader(f.OutboundBody))
	require.NoError(t, err)
	_, err = channel.DoRequest(f.Ctx, req, f.Info)
	require.Error(t, err)
	require.False(t, f.Ctx.GetBool("upstream_attempted"))
	require.Zero(t, accepted.Load())
	service.MarkTokenInterrupted(f.Ctx)
	// Exercise the settlement guard as well as the normal failed-request refund.
	service.PostTextConsumeQuota(f.Ctx, f.Info, &dto.Usage{}, nil)
	f.Info.Billing.Refund(f.Ctx)
	f.Info.Billing.Refund(f.Ctx)
	row := reservationOf(t, f.RequestID)
	require.Zero(t, row.Applied)
	require.Zero(t, row.Target)
	require.Equal(t, walletState{u.Balance, u.Balance}, walletOf(t, u))
	_, ok := markerOf(t, f.RequestID)
	require.False(t, ok)
}

func TestR7CompletedProxySettlement(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, stream := range []bool{false, true} {
		name := "non-stream"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			newHardeningDB(t)
			configureTieredModel(t, h2ModelA, fixtureAExpr)

			u := createHardeningUser(t, model.DB, "r7-completed-"+name, 10000)
			f := startHardeningFlow(t, u, h2ChannelA, h2ModelA, h2OutboundBodyA)
			f.Info.IsStream, f.Info.DisablePing = stream, true
			const payload = `{"id":"synthetic-r7","model":"gpt-6-luna","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`
			var accepted atomic.Int64
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				accepted.Add(1)
				body := payload
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					body = "data: " + payload + "\n\ndata: [DONE]\n\n"
				}
				_, _ = io.WriteString(w, body)
			}))
			defer mock.Close()
			req, err := http.NewRequest(http.MethodPost, mock.URL, strings.NewReader(f.OutboundBody))
			require.NoError(t, err)
			resp, err := channel.DoRequest(f.Ctx, req, f.Info)
			require.NoError(t, err)
			require.True(t, f.Ctx.GetBool("upstream_attempted"))
			require.EqualValues(t, 1, accepted.Load())
			var usage *dto.Usage
			if stream {
				result, apiErr := openai.OaiStreamHandler(f.Ctx, f.Info, resp)
				require.Nil(t, apiErr)
				usage = result
			} else {
				result, apiErr := openai.OpenaiHandler(f.Ctx, f.Info, resp)
				require.Nil(t, apiErr)
				usage = result
			}
			require.False(t, service.TokenObservationForLog(f.Ctx, false).Interrupted)
			service.PostTextConsumeQuota(f.Ctx, f.Info, usage, nil)
			row := reservationOf(t, f.RequestID)
			require.Equal(t, "settled", row.State)
			require.Equal(t, 2, row.Applied) // (100 * .02 + 20 * .10) * .5.
			require.Equal(t, walletState{u.Balance - 2, u.Balance - 2}, walletOf(t, u))
			_, ok := markerOf(t, f.RequestID)
			require.False(t, ok)
		})
	}
}
