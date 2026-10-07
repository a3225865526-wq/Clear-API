package routechain

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// Isolate legacy finance configuration from the existing durable test suite.
// The child uses the same race-instrumented test executable when run with -race.
func TestStage72LegacyNetwork(t *testing.T) {
	if os.Getenv("STAGE72_LEGACY_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^(TestStage72LegacyNetwork|TestE2EOrdinary503DoesNotFailover|TestE2EPostDispatchDisconnectDoesNotFailover|TestE2ESSEPostOutputDoesNotFailover|TestE2EToolChainNeverAdvances)$", "-test.v", "-test.timeout=120s")
		cmd.Env = append(os.Environ(), "STAGE72_LEGACY_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("legacy network subprocess: %v\n%s", err, out)
		}
		t.Log(string(out))
		return
	}
	const rejection = `{"error":{"type":"server_error","code":"pool_no_eligible_account","message":"no eligible pool account"}}`
	const spoof = `{"error":{"type":"server_error","code":"pool_no_eligible_account","message":"provider","origin":"local_executor"}}`
	for _, tc := range []struct {
		name               string
		body               string
		header, api, multi bool
		safe               bool
	}{
		{"E5 single trusted zero", rejection, true, false, false, true},
		{"E6 cross source success", rejection, true, false, true, true},
		{"E7 provider same code", rejection, false, false, true, false},
		{"E8 body origin spoof", spoof, false, false, true, false},
		{"E9 stripped header", spoof, false, false, true, false},
		{"E10 API header spoof", spoof, true, true, true, false},
		{"wrong status", rejection, true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			require.False(t, common.FinanceEnabled())
			if !tc.api {
				h.setPoolPublication(t)
			}
			statusA := 503
			if tc.name == "wrong status" {
				statusA = 500
			}
			behavior := mockBehavior{status: statusA, body: tc.body}
			if tc.header {
				behavior.headers = executorHeaderFixture
			}
			a := newMockUpstream(t, behavior)
			b := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
			settings := `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`
			kind := model.RouteChainCandidateTypePool
			if tc.api {
				kind = model.RouteChainCandidateTypeAPIChannel
				settings = ""
			}
			h.addChannel(21, a.srv.URL, settings)
			candidates := []model.RouteChainCandidate{{CandidateType: kind, TargetId: 21}}
			if tc.multi {
				h.addChannel(22, b.srv.URL, "")
				candidates = append(candidates, model.RouteChainCandidate{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22})
			}
			h.setChain(t, !tc.api, candidates)
			before := h.userQuota()
			status, _, _ := h.postChat(t, e2eModel, "", false)
			require.Equal(t, 1, a.count())
			if tc.safe && tc.multi {
				require.Equal(t, http.StatusOK, status)
				require.Equal(t, 1, b.count())
				h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled, []string{model.RouteChainTerminalAuthoritativeRej, model.RouteChainTerminalSucceeded})
				require.EqualValues(t, 1, h.eventuallyConsumeLogs(1))
				// Fixture usage 10 input + 5 output * completion ratio 2.
				require.Equal(t, 20, before-h.userQuota(), "exactly one successful usage charge")
			} else if tc.safe {
				require.Equal(t, http.StatusServiceUnavailable, status)
				require.Equal(t, 0, b.count())
				h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingRefunded, []string{model.RouteChainTerminalAuthoritativeRej})
				require.Eventually(t, func() bool { return h.userQuota() == before }, 3e9, 1e7, "full legacy refund")
				require.EqualValues(t, 0, h.countConsumeLogs())
			} else {
				require.NotEqual(t, http.StatusOK, status)
				require.Equal(t, 0, b.count())
				h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingSettledUncertain, []string{model.RouteChainTerminalOutcomeUncertain})
			}
			if tc.safe {
				attempt := h.ledgerAttempts(t)[0]
				require.Equal(t, "unavailable", attempt.UsageState)
				require.Equal(t, "health_neutral", attempt.HealthOutcome)
				require.Equal(t, model.RouteChainCostZeroConfirmed, attempt.ProviderCostState)
				require.False(t, attempt.UsageObserved)
				require.False(t, attempt.WorkObserved)
			}
			require.EqualValues(t, 0, h.countReservations(), "durable wallet OFF")
			require.Len(t, h.ledgerDecisions(t), 1)
		})
	}
	if os.Getenv("STAGE72_EXECUTOR_TEST_BINARY") != "" {
		t.Run("cross module executor", testStage72CrossModule)
	}
}

func testStage72CrossModule(t *testing.T) {
	addressFile := filepath.Join(t.TempDir(), "executor-address")
	cmd := exec.Command(os.Getenv("STAGE72_EXECUTOR_TEST_BINARY"), "-test.run=^TestStage72ExecutorBridge$", "-test.timeout=90s")
	cmd.Env = append(os.Environ(), "STAGE72_BRIDGE_ADDRESS_FILE="+addressFile)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var address string
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(addressFile)
		address = string(b)
		return err == nil && strings.HasPrefix(address, "http://127.0.0.1:")
	}, 10*time.Second, 20*time.Millisecond)
	for _, mode := range []string{"local-single", "local-multi", "spoof"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			h.setPoolPublication(t)
			path := "local"
			if mode == "spoof" {
				path = "spoof"
			}
			h.addChannel(21, address+"/"+path, `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`)
			b := newMockUpstream(t, mockBehavior{body: chatOKBody("from-b")})
			candidates := []model.RouteChainCandidate{{CandidateType: model.RouteChainCandidateTypePool, TargetId: 21}}
			if mode != "local-single" {
				h.addChannel(22, b.srv.URL, "")
				candidates = append(candidates, model.RouteChainCandidate{CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22})
			}
			h.setChain(t, true, candidates)
			before := h.userQuota()
			status, _, _ := h.postChat(t, e2eModel, "", false)
			switch mode {
			case "local-single":
				require.Equal(t, 503, status)
				require.Equal(t, 0, b.count())
				h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingRefunded, []string{model.RouteChainTerminalAuthoritativeRej})
				require.Eventually(t, func() bool { return h.userQuota() == before }, 3*time.Second, 10*time.Millisecond)
			case "local-multi":
				require.Equal(t, 200, status)
				require.Equal(t, 1, b.count())
				h.assertLedgerShape(t, model.RouteChainRequestFinalSuccess, model.RouteChainBillingSettled, []string{model.RouteChainTerminalAuthoritativeRej, model.RouteChainTerminalSucceeded})
				require.Equal(t, 20, before-h.userQuota())
				require.EqualValues(t, 1, h.eventuallyConsumeLogs(1))
			case "spoof":
				require.Equal(t, 503, status)
				require.Equal(t, 0, b.count())
				h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingSettledUncertain, []string{model.RouteChainTerminalOutcomeUncertain})
			}
			resp, err := http.Get(address + "/counts")
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			want := `"provider_calls":0`
			if mode == "spoof" {
				want = `"provider_calls":1`
			}
			require.Contains(t, string(body), want)
		})
	}
}
