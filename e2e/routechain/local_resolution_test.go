package routechain

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalResolutionNetworkRefund(t *testing.T) {
	if os.Getenv("STAGE72_LEGACY_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLocalResolutionNetworkRefund$", "-test.v")
		cmd.Env = append(os.Environ(), "STAGE72_LEGACY_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated legacy proof: %v\n%s", err, out)
		}
		t.Log(string(out))
		return
	}
	for _, mode := range []string{"trusted-local", "upstream-no-proof", "API-header-spoof", "missing-phase", "unknown-transport"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			headers := map[string]string{types.ExecutorErrorOriginHeader: "local", types.ExecutorDispatchPhaseHeader: "model_resolution", types.ExecutorProviderTransportHeader: "not_started"}
			if mode == "upstream-no-proof" {
				headers = nil
			}
			if mode == "missing-phase" {
				delete(headers, types.ExecutorDispatchPhaseHeader)
			}
			if mode == "unknown-transport" {
				headers[types.ExecutorProviderTransportHeader] = "unknown"
			}
			a := newMockUpstream(t, mockBehavior{status: 400, body: `{"error":{"type":"invalid_request_error","code":"model_not_found","message":"mock resolver rejection"}}`, headers: headers})
			settings := ""
			kind := model.RouteChainCandidateTypeAPIChannel
			if mode != "API-header-spoof" {
				h.setPoolPublication(t)
				settings = `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`
				kind = model.RouteChainCandidateTypePool
			}
			h.addChannel(21, a.srv.URL, settings)
			h.setChain(t, mode != "API-header-spoof", []model.RouteChainCandidate{{CandidateType: kind, TargetId: 21}})
			before := h.userQuota()
			status, _, _ := h.postChat(t, e2eModel, "", false)
			require.Equal(t, http.StatusBadRequest, status)
			require.Equal(t, 1, a.count())
			if mode == "trusted-local" {
				require.Equal(t, before, h.userQuota())
				h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingRefunded, []string{model.RouteChainTerminalAuthoritativeRej})
			} else {
				require.Less(t, h.userQuota(), before)
				h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingSettledUncertain, []string{model.RouteChainTerminalOutcomeUncertain})
			}
			require.EqualValues(t, 0, h.countConsumeLogs())
			require.Len(t, h.ledgerDecisions(t), 1)
			require.Equal(t, "stop", h.ledgerDecisions(t)[0].Decision)
		})
	}
	if os.Getenv("STAGE72_EXECUTOR_TEST_BINARY") != "" {
		t.Run("real executor resolver boundary", testLocalResolutionCrossModule)
	}
}

func testLocalResolutionCrossModule(t *testing.T) {
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
	h := newHarness(t)
	h.setPoolPublication(t)
	h.addChannel(21, address+"/resolution", `{"upstream_pool":"e2e-pool","pool_routing_enabled":true}`)
	b := newMockUpstream(t, mockBehavior{body: chatOKBody("must-not-replay")})
	h.addChannel(22, b.srv.URL, "")
	h.setChain(t, true, []model.RouteChainCandidate{{CandidateType: model.RouteChainCandidateTypePool, TargetId: 21}, {CandidateType: model.RouteChainCandidateTypeAPIChannel, TargetId: 22}})
	before := h.userQuota()
	status, _, _ := h.postChat(t, e2eModel, "", false)
	require.Equal(t, 400, status)
	require.Zero(t, b.count())
	require.Eventually(t, func() bool { return h.userQuota() == before }, 3*time.Second, 10*time.Millisecond)
	h.assertLedgerShape(t, model.RouteChainRequestFinalFailed, model.RouteChainBillingRefunded, []string{model.RouteChainTerminalAuthoritativeRej})
	require.EqualValues(t, 0, h.countConsumeLogs())
	require.Len(t, h.ledgerDecisions(t), 1)
	resp, err := http.Get(address + "/counts")
	require.NoError(t, err)
	defer resp.Body.Close()
	counts, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(counts), `"provider_calls":0`)
}
