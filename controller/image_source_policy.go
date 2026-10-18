package controller

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/imageexecution"

	"github.com/gin-gonic/gin"
)

// Admin image source policy endpoints. They sit behind PoolOwnerAuth (root
// user ID 1 only) exactly like the rest of the pool control plane. They never
// grant account trust, capability or publication, and they never let an
// ordinary user influence which source serves the request.

func imagePolicyError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "IMAGE_POLICY_INTERNAL"
	switch {
	case errors.Is(err, model.ErrImageSourcePolicyInvalid):
		status, code = http.StatusBadRequest, "IMAGE_POLICY_INVALID"
	case errors.Is(err, model.ErrPoolConflict):
		status, code = http.StatusConflict, "IMAGE_POLICY_CONFLICT"
	case errors.Is(err, model.ErrPoolNotFound):
		status, code = http.StatusNotFound, "IMAGE_POLICY_NOT_FOUND"
	case errors.Is(err, model.ErrPoolUnavailable):
		status, code = http.StatusServiceUnavailable, "IMAGE_POLICY_UNAVAILABLE"
	}
	c.JSON(status, gin.H{"success": false, "code": code})
}

// ListImageSourcePolicies returns every configured operator decision.
func ListImageSourcePolicies(c *gin.Context) {
	policies, err := model.ListImageSourcePolicies()
	if err != nil {
		imagePolicyError(c, err)
		return
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].Model < policies[j].Model })
	c.JSON(http.StatusOK, gin.H{"success": true, "data": policies})
}

// SaveImageSourcePolicy writes one model's source policy with CAS and a
// read-back. The four presets are the complete supported set; anything else is
// rejected instead of being coerced.
func SaveImageSourcePolicy(c *gin.Context) {
	var input struct {
		Model           string `json:"model"`
		Strategy        string `json:"strategy"`
		Group           string `json:"group"`
		Enabled         *bool  `json:"enabled"`
		ExpectedVersion int    `json:"expected_version"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		imagePolicyError(c, model.ErrImageSourcePolicyInvalid)
		return
	}
	policy, err := model.SaveImageSourcePolicy(model.ImageSourcePolicy{
		Model: input.Model, Strategy: input.Strategy, Group: input.Group, Enabled: *input.Enabled,
	}, input.ExpectedVersion)
	if err != nil {
		imagePolicyError(c, err)
		return
	}
	logger.LogInfo(c, "image source policy saved model="+policy.Model+" strategy="+policy.Strategy)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": policy})
}

// GetImageSourcePolicy reports one model's decision and the chain shape it
// would need. It never mutates routing.
func GetImageSourcePolicy(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		imagePolicyError(c, model.ErrImageSourcePolicyInvalid)
		return
	}
	policy, err := model.GetImageSourcePolicy(modelName)
	if err != nil {
		imagePolicyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"policy": policy}})
}

// GetImageRequestStatus is the authenticated status endpoint a client uses to
// resolve an UNKNOWN result. It only returns the caller's own request and never
// exposes account identity, source topology or internal policy.
func GetImageRequestStatus(c *gin.Context) {
	requestID := c.Param("request")
	if !imageexecution.ParseEntityID(requestID) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "invalid_request_id", "type": "invalid_request_error"}})
		return
	}
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	owner, ok := imageexecution.OwnerID(requestID)
	if !ok || owner != userID {
		// Do not reveal whether another user's request exists.
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": gin.H{"code": "image_request_not_found", "type": "invalid_request_error"}})
		return
	}
	view, apiErr := imageexecution.LoadStatus(requestID)
	if apiErr != nil {
		c.JSON(apiErr.StatusCode, gin.H{"success": false, "error": gin.H{"code": "image_request_unavailable", "type": "image_execution_error"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// ListPendingImageExecutions is the owner-only reconciliation view for requests
// that still own an open reservation. It is deliberately read-only: resolving an
// UNKNOWN requires an explicit, audited decision, so this endpoint never
// refunds, retries, settles or re-dispatches anything. It exposes request
// identity, state and billing markers only — never prompts or image bytes.
func ListPendingImageExecutions(c *gin.Context) {
	items, err := model.ImageExecutionsPending(200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "IMAGE_RECONCILIATION_UNAVAILABLE"})
		return
	}
	views := make([]model.ImageExecutionJSON, 0, len(items))
	for i := range items {
		attempts, attemptErr := model.ListImageExecutionAttempts(items[i].RequestID)
		if attemptErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "IMAGE_RECONCILIATION_UNAVAILABLE"})
			return
		}
		views = append(views, model.ProjectImageExecution(&items[i], attempts))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": views})
}
