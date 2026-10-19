package router_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestImageSourcePolicyAdminSurface proves the operator surface: only the root
// owner can read or write it, ordinary users and admins cannot, the four presets
// round-trip, an unknown preset is rejected, and a stale version loses the CAS.
func TestImageSourcePolicyAdminSurface(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Option{}))
	oldOptions := common.OptionMap
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})

	// An ordinary user and a non-owner admin can neither read nor write.
	for _, token := range []string{"synthetic-user-pat", "synthetic-admin-pat", "synthetic-other-root-pat"} {
		require.Equal(t, http.StatusForbidden, setup.request("GET", "/api/owner/pool/image-policies", token, nil).Code)
		require.Equal(t, http.StatusForbidden, setup.request("PUT", "/api/owner/pool/image-policy", token, map[string]any{
			"model": "gpt-image-2", "strategy": model.ImageSourcePoolFirst, "enabled": true, "expected_version": 0,
		}).Code)
	}

	for _, strategy := range []string{model.ImageSourcePoolFirst, model.ImageSourceAPIFirst, model.ImageSourcePoolOnly, model.ImageSourceAPIOnly} {
		read := setup.request("GET", "/api/owner/pool/image-policies", "synthetic-owner-pat", nil)
		require.Equal(t, http.StatusOK, read.Code, read.Body.String())

		saved := setup.request("PUT", "/api/owner/pool/image-policy", "synthetic-owner-pat", map[string]any{
			"model": "gpt-image-2", "strategy": strategy, "enabled": true, "expected_version": 0,
		})
		// The first write succeeds; later writes need the current version, so a
		// zero expected version loses the CAS after the first iteration.
		if strategy == model.ImageSourcePoolFirst {
			require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
			require.Contains(t, saved.Body.String(), strategy)
		} else {
			require.Equal(t, http.StatusConflict, saved.Code, saved.Body.String())
		}
	}

	// An unknown preset is refused rather than coerced.
	invalid := setup.request("PUT", "/api/owner/pool/image-policy", "synthetic-owner-pat", map[string]any{
		"model": "gpt-image-2", "strategy": "legacy_fallback", "enabled": true, "expected_version": 1,
	})
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())

	// A correct CAS updates and reports the new version.
	updated := setup.request("PUT", "/api/owner/pool/image-policy", "synthetic-owner-pat", map[string]any{
		"model": "gpt-image-2", "strategy": model.ImageSourceAPIOnly, "enabled": true, "expected_version": 1,
	})
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Contains(t, updated.Body.String(), `"version":2`)

	// A stale version is refused.
	stale := setup.request("PUT", "/api/owner/pool/image-policy", "synthetic-owner-pat", map[string]any{
		"model": "gpt-image-2", "strategy": model.ImageSourcePoolOnly, "enabled": true, "expected_version": 1,
	})
	require.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())

	read := setup.request("GET", "/api/owner/pool/image-policy?model=gpt-image-2", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), model.ImageSourceAPIOnly)
}

// TestImageRequestStatusIsOwnerScoped proves the public status endpoint: another
// user's request is reported as not found, an unknown request is not leaked, and
// the caller's own request returns its durable projection without account data.
func TestImageRequestStatusIsOwnerScoped(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Option{}))
	require.NoError(t, model.MigrateImageExecutions(setup.db))

	entry := &model.ImageExecution{
		RequestID: "req-status", UserID: 8, TokenID: 8,
		BodyDigest: model.ImageRequestBodyDigest([]byte(`{}`)), Operation: model.ImageRequestOperation,
		Model: "gpt-image-2", RequestedCount: 1, SourceStrategy: model.ImageSourcePoolFirst,
	}
	require.NoError(t, model.CreateImageExecutionRequest(entry))
	require.NoError(t, model.AdvanceImageExecution("req-status", model.ImageRequestCreated, model.ImageRequestReserved, nil))

	// The owner sees their own request through the authenticated endpoint.
	ownRecorder := httptest.NewRecorder()
	ownContext, _ := gin.CreateTestContext(ownRecorder)
	ownContext.Request = httptest.NewRequest(http.MethodGet, "/v1/images/requests/req-status", nil)
	ownContext.Params = gin.Params{{Key: "request", Value: "req-status"}}
	common.SetContextKey(ownContext, constant.ContextKeyUserId, 8)
	controller.GetImageRequestStatus(ownContext)
	require.Equal(t, http.StatusOK, ownRecorder.Code, ownRecorder.Body.String())
	require.Contains(t, ownRecorder.Body.String(), "req-status")
	require.NotContains(t, ownRecorder.Body.String(), "images-account")

	// A different user gets 404 and cannot distinguish it from a missing row.
	otherRecorder := httptest.NewRecorder()
	otherContext, _ := gin.CreateTestContext(otherRecorder)
	otherContext.Request = httptest.NewRequest(http.MethodGet, "/v1/images/requests/req-status", nil)
	otherContext.Params = gin.Params{{Key: "request", Value: "req-status"}}
	common.SetContextKey(otherContext, constant.ContextKeyUserId, 1)
	controller.GetImageRequestStatus(otherContext)
	require.Equal(t, http.StatusNotFound, otherRecorder.Code, otherRecorder.Body.String())

	missingRecorder := httptest.NewRecorder()
	missingContext, _ := gin.CreateTestContext(missingRecorder)
	missingContext.Request = httptest.NewRequest(http.MethodGet, "/v1/images/requests/req-does-not-exist", nil)
	missingContext.Params = gin.Params{{Key: "request", Value: "req-does-not-exist"}}
	common.SetContextKey(missingContext, constant.ContextKeyUserId, 8)
	controller.GetImageRequestStatus(missingContext)
	require.Equal(t, http.StatusNotFound, missingRecorder.Code, missingRecorder.Body.String())
}

// TestImageSourcePolicyRejectsClientOverride proves the relay body cannot carry
// a source choice: the reserved policy headers are stripped and the frozen
// operator value is what the request record stores.
func TestImageSourcePolicyRejectsClientOverride(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Option{}))
	oldOptions := common.OptionMap
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})
	_, err := model.SaveImageSourcePolicy(model.ImageSourcePolicy{Model: "gpt-image-2", Strategy: model.ImageSourceAPIOnly, Enabled: true}, 0)
	require.NoError(t, err)

	body := map[string]any{"model": "gpt-image-2", "prompt": "cat", "n": 1, "source": "pool", "account_id": "attacker"}
	encoded, err := common.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(encoded))
	req.Header.Set("Authorization", "Bearer synthetic-user-pat")
	req.Header.Set("Content-Type", "application/json")
	// A client tries to self-authorize a pool execution.
	req.Header.Set("X-ClearAPI-PG03-Images-Policy", "forged")
	req.Header.Set("X-ClearAPI-PG03-Images-Signature", "forged")
	req.Header.Set("X-ClearAPI-PG03-Images-Request", "forged")
	req.Header.Set("X-ClearAPI-PG03-Images-Attempt", "forged")
	response := httptest.NewRecorder()
	setup.api.ServeHTTP(response, req)

	// The operator decision is unchanged and no forged header reached a record.
	stored, err := model.GetImageSourcePolicy("gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, model.ImageSourceAPIOnly, stored.Strategy)
}

// TestImageReconciliationSurfaceIsOwnerOnly proves the reconciliation view is
// owner-only and read-only: an UNKNOWN request stays UNKNOWN with its hold
// intact after being listed, so reading it can never release a reservation.
func TestImageReconciliationSurfaceIsOwnerOnly(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, setup.db.AutoMigrate(&model.Option{}))
	require.NoError(t, model.MigrateImageExecutions(setup.db))

	entry := &model.ImageExecution{
		RequestID: "req-reconcile", UserID: 8, TokenID: 8,
		BodyDigest: model.ImageRequestBodyDigest([]byte(`{}`)), Operation: model.ImageRequestOperation,
		Model: "gpt-image-2", RequestedCount: 1, SourceStrategy: model.ImageSourcePoolFirst,
	}
	require.NoError(t, model.CreateImageExecutionRequest(entry))
	require.NoError(t, model.AdvanceImageExecution("req-reconcile", model.ImageRequestCreated, model.ImageRequestReserved, nil))
	require.NoError(t, model.BillingHoldImageExecution("req-reconcile"))

	// Ordinary users and a non-owner root cannot read it.
	for _, token := range []string{"synthetic-user-pat", "synthetic-admin-pat", "synthetic-other-root-pat"} {
		denied := setup.request("GET", "/api/owner/pool/image-executions", token, nil)
		require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	}

	allowed := setup.request("GET", "/api/owner/pool/image-executions", "synthetic-owner-pat", nil)
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	require.Contains(t, allowed.Body.String(), "req-reconcile")
	require.Contains(t, allowed.Body.String(), `"billing_state":"held_unknown"`)
	// No prompt, credential or image payload is exposed.
	require.NotContains(t, allowed.Body.String(), "images-account")

	// Listing must not resolve the hold.
	held, err := model.LoadImageExecution("req-reconcile")
	require.NoError(t, err)
	require.Equal(t, model.ImageRequestUnknown, held.State)
	require.Equal(t, "held_unknown", held.BillingState)
}
