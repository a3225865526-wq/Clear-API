package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/accountpool"
	"github.com/gin-gonic/gin"
)

// Site-owner upstream account pool handlers. Every route in this group is
// already gated by middleware.PoolOwnerAuth (fixed owner ID 1 + root role);
// these handlers never trust a client-supplied user id. All outputs are
// non-secret projections: raw identities, codes, callbacks, cookies, tokens
// and executor response bodies never reach a response, log or audit entry.

type poolTaskView struct {
	Id         int    `json:"id"`
	Platform   string `json:"platform"`
	AccessType string `json:"access_type"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	FailReason string `json:"fail_reason,omitempty"`
	ExpiresAt  int64  `json:"expires_at"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

func poolTaskViews(tasks []model.UpstreamPoolAuthTask) []poolTaskView {
	views := make([]poolTaskView, 0, len(tasks))
	for _, task := range tasks {
		views = append(views, poolTaskView{
			Id:         task.Id,
			Platform:   task.Platform,
			AccessType: task.AccessType,
			Kind:       task.Kind,
			Status:     task.Status,
			FailReason: task.FailReason,
			ExpiresAt:  task.ExpiresAt,
			CreatedAt:  task.CreatedAt,
			UpdatedAt:  task.UpdatedAt,
		})
	}
	return views
}

func poolError(c *gin.Context, err error) {
	// Fixed codes only: no error text, SQL, executor bodies or identities.
	status, code := http.StatusInternalServerError, "POOL_INTERNAL"
	switch {
	case errors.Is(err, model.ErrPoolInvalid), errors.Is(err, accountpool.ErrPoolInvalid):
		status, code = http.StatusBadRequest, "POOL_INVALID"
	case errors.Is(err, model.ErrPoolNotFound), errors.Is(err, accountpool.ErrPoolNotFound):
		status, code = http.StatusNotFound, "POOL_NOT_FOUND"
	case errors.Is(err, model.ErrPoolConflict), errors.Is(err, accountpool.ErrPoolConflict),
		errors.Is(err, accountpool.ErrPoolTaskConsumed), errors.Is(err, accountpool.ErrPoolIdentityMismatch):
		status, code = http.StatusConflict, "POOL_CONFLICT"
	case errors.Is(err, accountpool.ErrPoolTaskExpired):
		status, code = http.StatusGone, "POOL_TASK_EXPIRED"
	case errors.Is(err, accountpool.ErrPoolNotAdmitted):
		status, code = http.StatusConflict, "POOL_NOT_ADMITTED"
	case errors.Is(err, accountpool.ErrPoolIsolationReq):
		status, code = http.StatusConflict, "POOL_ISOLATION_REQUIRED"
	case errors.Is(err, accountpool.ErrPoolCorrelationFail):
		status, code = http.StatusBadGateway, "POOL_CORRELATION_FAILED"
	case errors.Is(err, accountpool.ErrPoolAuthTimeout):
		status, code = http.StatusGatewayTimeout, "POOL_AUTH_TIMEOUT"
	case errors.Is(err, accountpool.ErrPoolUnsupported):
		status, code = http.StatusNotImplemented, "POOL_UNSUPPORTED"
	case errors.Is(err, accountpool.ErrPoolExecutorOffline):
		status, code = http.StatusBadGateway, "POOL_EXECUTOR_OFFLINE"
	case errors.Is(err, accountpool.ErrPoolExecutorFailed):
		status, code = http.StatusBadGateway, "POOL_EXECUTOR_FAILED"
	case errors.Is(err, model.ErrPoolUnavailable), errors.Is(err, accountpool.ErrPoolUnavailable):
		status, code = http.StatusServiceUnavailable, "POOL_UNAVAILABLE"
	}
	c.JSON(status, gin.H{"success": false, "code": code})
}

func poolReady(c *gin.Context) bool {
	if err := model.UpstreamPoolReady(model.DB); err != nil {
		poolError(c, err)
		return false
	}
	return true
}

func poolAccountID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		poolError(c, model.ErrPoolInvalid)
		return 0, false
	}
	return id, true
}

func PoolCapability(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"can_access":      true,
		"schema_ready":    model.UpstreamPoolReady(model.DB) == nil,
		"adapter_version": accountpool.AdapterVersion,
	}})
}

func GetPoolFallbackPolicy(c *gin.Context) {
	poolID := c.Query("pool_id")
	modelName := c.Query("model")
	if poolID == "" || modelName == "" {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	policy, err := accountpool.GetFallbackPolicy(c.Request.Context(), poolID, modelName)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": policy})
}

func SavePoolFallbackPolicy(c *gin.Context) {
	var input accountpool.FallbackPolicyInput
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	policy, err := accountpool.SaveFallbackPolicy(c.Request.Context(), input)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": policy})
}

func ListPoolExecutors(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": accountpool.Executors()})
}

func GetPoolCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"adapter_version": accountpool.AdapterVersion,
		"platforms":       accountpool.Catalog(),
	}})
}

func ListPoolAccounts(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	accounts, err := accountpool.ListAccounts(c.Query("platform"), c.Query("admin_status"), c.Query("run_status"))
	if err != nil {
		poolError(c, err)
		return
	}
	type accountView struct {
		model.UpstreamPoolAccount
		Quota accountpool.QuotaView `json:"quota"`
	}
	views := make([]accountView, 0, len(accounts))
	for i := range accounts {
		quota, err := accountpool.QuotaForAccount(&accounts[i], false)
		if err != nil {
			poolError(c, err)
			return
		}
		views = append(views, accountView{UpstreamPoolAccount: accounts[i], Quota: quota})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": views})
}

func CreatePoolAccount(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	var input accountpool.CreateInput
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	account, err := accountpool.CreateAccount(c.GetInt("id"), input)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": account})
}

func GetPoolAccount(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	result, err := accountpool.GetAccount(id)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"account": result.Account,
		"tasks":   poolTaskViews(result.Tasks),
	}})
}

func GetPoolAccountQuota(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	account, err := accountpool.GetAccount(id)
	if err != nil {
		poolError(c, err)
		return
	}
	quota, err := accountpool.QuotaForAccount(&account.Account, true)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": quota})
}

func GetPoolAccountCapabilities(c *gin.Context) {
	if len(c.Query("model")) > 200 {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	result, err := accountpool.GetAccount(id)
	if err != nil {
		poolError(c, err)
		return
	}
	platform, _, known := accountpool.Lookup(result.Account.Platform, result.Account.AccessType)
	if !known {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	var accessSpec *accountpool.AccessTypeSpec
	for i := range platform.AccessTypes {
		if platform.AccessTypes[i].Type == result.Account.AccessType {
			accessSpec = &platform.AccessTypes[i]
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"platform":        platform,
		"access_type":     accessSpec,
		"account_status":  result.Account.RunStatus,
		"model_admission": accountpool.ModelAdmissionFor(result.Account.Platform, result.Account.AccessType, c.Query("model")),
	}})
}

func StartPoolAuth(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	var input struct {
		Kind string `json:"kind"`
	}
	_ = common.UnmarshalBodyReusable(c, &input)
	view, err := accountpool.StartAuth(c.GetInt("id"), id, input.Kind)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func StartPoolReauth(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	view, err := accountpool.StartAuth(c.GetInt("id"), id, accountpool.AuthKindReauth)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func CompletePoolAuth(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	var input accountpool.CompleteInput
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	account, err := accountpool.CompleteAuth(c.GetInt("id"), id, input)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": account})
}

func CancelPoolAuth(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	var input struct {
		TaskId int `json:"task_id"`
	}
	if err := common.UnmarshalBodyReusable(c, &input); err != nil || input.TaskId <= 0 {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	if err := accountpool.CancelAuth(c.GetInt("id"), id, input.TaskId); err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"cancelled": true}})
}

func PausePoolAccount(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedVersion int `json:"expected_version"`
	}
	if err := common.UnmarshalBodyReusable(c, &input); err != nil || input.ExpectedVersion <= 0 {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	account, err := accountpool.PauseAccount(c.GetInt("id"), id, input.ExpectedVersion)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": account})
}

func ResumePoolAccount(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedVersion int `json:"expected_version"`
	}
	if err := common.UnmarshalBodyReusable(c, &input); err != nil || input.ExpectedVersion <= 0 {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	account, err := accountpool.ResumeAccount(c.GetInt("id"), id, input.ExpectedVersion)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": account})
}

func DeletePoolAccount(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	if err := accountpool.DeleteAccount(c.GetInt("id"), id); err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"deleted": true}})
}

func SyncPoolAccount(c *gin.Context) {
	if !poolReady(c) {
		return
	}
	id, ok := poolAccountID(c)
	if !ok {
		return
	}
	account, err := accountpool.SyncAccount(c.GetInt("id"), id)
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": account})
}
