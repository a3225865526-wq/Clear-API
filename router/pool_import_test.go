package router_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/accountpool"
	"github.com/stretchr/testify/require"
)

type importExecutor struct {
	*mockPoolExecutor
	importCalls int
	imported    []accountpool.CredentialImportRequest
}

func (e *importExecutor) ImportCredential(_ context.Context, req accountpool.CredentialImportRequest) (*accountpool.CredentialImportResult, error) {
	e.importCalls++
	e.imported = append(e.imported, req)
	return &accountpool.CredentialImportResult{CredentialRef: req.FileName, CredentialDigest: "synthetic-canonical-digest", Provider: "codex"}, nil
}

func (e *importExecutor) DeleteImportedCredential(context.Context, string) error { return nil }

func (e *importExecutor) PoolModelSnapshot(context.Context) (accountpool.PoolExecutorModelSnapshot, error) {
	return accountpool.PoolExecutorModelSnapshot{
		Identities:   []accountpool.PoolExecutorTrustIdentity{{AuthID: "synthetic-import.json", Provider: "codex", CredentialDigest: "synthetic-canonical-digest", State: "trusted"}},
		Capabilities: []accountpool.PoolExecutorCapability{{AuthID: "synthetic-import.json", Provider: "codex", CredentialDigest: "synthetic-canonical-digest", Model: "synthetic-model", State: "verified"}},
	}, nil
}

func TestPoolJSONImportRoutesThroughExecutorAndRegistersAccount(t *testing.T) {
	setup := newPoolTestSetup(t, true)
	require.NoError(t, model.MigrateUpstreamPool(setup.db))
	executor := &importExecutor{mockPoolExecutor: setup.executor}
	setup.executor = nil
	previousFactory := accountpool.ExecutorFactory
	accountpool.ExecutorFactory = func(id string) (accountpool.ExecutorClient, error) {
		if id == accountpool.ExecutorGrok2API {
			return accountpool.NewOfflineExecutor(), nil
		}
		return executor, nil
	}
	t.Cleanup(func() { accountpool.ExecutorFactory = previousFactory })
	content := `{"type":"codex","account_id":"synthetic-import-account","chatgpt_account_id":"synthetic-import-account","email":"synthetic-import@example.com","plan_type":"free","access_token":"synthetic-access","refresh_token":"synthetic-refresh","id_token":"synthetic-id"}`
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "synthetic-import.json")
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/owner/pool/accounts/import", &body)
	req.Header.Set("Authorization", "Bearer synthetic-owner-pat")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	setup.api.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "synthetic-access")
	require.NotContains(t, rec.Body.String(), "synthetic-refresh")
	require.Equal(t, 1, executor.importCalls)
	var account model.UpstreamPoolAccount
	require.NoError(t, setup.db.Order("id desc").First(&account).Error)
	require.Contains(t, account.CredentialRef, "codex-")
	require.True(t, strings.HasSuffix(account.CredentialRef, ".json"))
	require.Equal(t, model.PoolRunStatusNotAdmitted, account.RunStatus)
	for _, token := range []string{"synthetic-user-pat", "synthetic-admin-pat"} {
		req := httptest.NewRequest(http.MethodPost, "/api/owner/pool/accounts/import", strings.NewReader("not multipart"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "multipart/form-data")
		rec := httptest.NewRecorder()
		setup.api.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
	require.Equal(t, 1, executor.importCalls)
}
