package accountpool

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const pg03SnapshotContext = "clearapi.pg03.snapshot"
const PG03PolicyHeader = "X-ClearAPI-PG03-Policy"
const PG03SignatureHeader = "X-ClearAPI-PG03-Signature"
const PG03RequestHeader = "X-ClearAPI-PG03-Request"

// Field order is the v1 canonical wire format, shared with the executor.
type pg03PolicySnapshot struct {
	PolicyID         int      `json:"policy_id"`
	PoolID           string   `json:"pool_id"`
	ModelName        string   `json:"model_name"`
	AccountOrder     []string `json:"account_order"`
	MaxFallbackCount int      `json:"max_fallback_count"`
	RequestID        string   `json:"request_id"`
	Method           string   `json:"method"`
	Path             string   `json:"path"`
	BodyDigest       string   `json:"body_digest"`
	IssuedAt         int64    `json:"issued_at"`
}

type pg03RequestSnapshot struct {
	policy    pg03PolicySnapshot
	channelID int
	model     string
	active    bool
	err       error
	payload   string
}

// FreezePG03Policy runs once at the Responses SSE entry, including absence and
// errors. Retries may not reread configuration or change the bound channel.
func FreezePG03Policy(c *gin.Context, channelID int, modelName string) error {
	if previous, exists := c.Get(pg03SnapshotContext); exists {
		state := previous.(*pg03RequestSnapshot)
		if state.active && (state.channelID != channelID || state.model != modelName) {
			return ErrPoolInvalid
		}
		return state.err
	}
	state := &pg03RequestSnapshot{channelID: channelID, model: modelName}
	c.Set(pg03SnapshotContext, state)
	state.err = freezePG03Policy(c, state)
	return state.err
}

func freezePG03Policy(c *gin.Context, state *pg03RequestSnapshot) error {
	channel, err := model.GetChannelById(state.channelID, true)
	if err != nil {
		return err
	}
	if !channel.PoolChannelBound() {
		return nil
	}
	poolID := ""
	settings := []string{channel.OtherSettings}
	if channel.Setting != nil {
		settings = append(settings, *channel.Setting)
	}
	for _, setting := range settings {
		var raw map[string]any
		if common.UnmarshalJsonStr(setting, &raw) != nil {
			continue
		}
		if value, exists := raw["upstream_pool"]; exists {
			id, ok := value.(string)
			if object, objectOK := value.(map[string]any); objectOK {
				id, ok = object["pool_id"].(string)
			}
			if !ok || id == "" || (poolID != "" && poolID != id) {
				return ErrPoolInvalid
			}
			poolID = id
		}
	}
	if poolID == "" {
		return ErrPoolInvalid
	}
	policy, err := GetFallbackPolicy(c.Request.Context(), poolID, state.model)
	if err != nil {
		return err
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		requestID = hex.EncodeToString(nonce[:])
	}
	if policy == nil || !policy.Enabled {
		// Automatic account selection uses the executor's existing scheduler.
		// It still requires the independent Gateway proof and one frozen request
		// identity, even when no per-model fallback policy was configured.
		state.policy = pg03PolicySnapshot{PoolID: poolID, ModelName: state.model, RequestID: requestID, IssuedAt: time.Now().Unix()}
		state.active = true
		return nil
	}
	if policy.MaxFallbackCount < 0 || policy.MaxFallbackCount > 3 {
		return ErrPoolInvalid
	}
	order := make([]string, 0, len(policy.Members))
	seen := make(map[string]bool)
	for _, member := range policy.Members {
		if !member.Enabled {
			continue
		}
		var account model.UpstreamPoolAccount
		if err := model.DB.WithContext(c.Request.Context()).First(&account, member.AccountId).Error; err != nil {
			return ErrPoolInvalid
		}
		ref := account.CredentialRef
		if account.Platform != "codex" || account.ExecutorId != ExecutorCliproxyAPI || ref == "" || ref != strings.TrimSpace(ref) || seen[ref] {
			return ErrPoolInvalid
		}
		// The executor reference must belong to exactly one registered row.
		var count int64
		if err := model.DB.WithContext(c.Request.Context()).Model(&model.UpstreamPoolAccount{}).Where("executor_id = ? AND credential_ref = ?", ExecutorCliproxyAPI, ref).Count(&count).Error; err != nil || count != 1 {
			return ErrPoolInvalid
		}
		seen[ref] = true
		order = append(order, ref)
	}
	if len(order) == 0 || len(order) > 4 {
		return ErrPoolInvalid
	}
	state.policy = pg03PolicySnapshot{PolicyID: policy.Id, PoolID: poolID, ModelName: state.model, AccountOrder: order, MaxFallbackCount: policy.MaxFallbackCount, RequestID: requestID, IssuedAt: time.Now().Unix()}
	state.active = true
	return nil
}

// ApplyPG03Policy runs after all upstream header overrides. Client/override
// values never authenticate a policy. The digest covers final outbound bytes.
func ApplyPG03Policy(c *gin.Context, req *http.Request) error {
	req.Header.Del(PG03PolicyHeader)
	req.Header.Del(PG03SignatureHeader)
	req.Header.Del(PG03RequestHeader)
	value, exists := c.Get(pg03SnapshotContext)
	if !exists {
		return nil
	}
	state := value.(*pg03RequestSnapshot)
	if state.err != nil {
		return state.err
	}
	if !state.active {
		return nil
	}
	key := os.Getenv("CLEARAPI_PG03_POLICY_KEY")
	if len(key) < 32 || req.Method != http.MethodPost || req.URL.Path != "/v1/responses" || req.GetBody == nil {
		return ErrPoolInvalid
	}
	body, err := req.GetBody()
	if err != nil {
		return err
	}
	defer body.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, body); err != nil {
		return err
	}
	policy := state.policy
	policy.Method, policy.Path, policy.BodyDigest = req.Method, req.URL.Path, hex.EncodeToString(digest.Sum(nil))
	payload, err := common.Marshal(policy)
	if err != nil {
		return err
	}
	canonical := string(payload)
	if state.payload != "" && state.payload != canonical {
		return errors.New("PG03 request binding changed")
	}
	state.payload = canonical
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("clearapi-pg03-policy-v1\n"))
	mac.Write(payload)
	if policy.PolicyID > 0 {
		req.Header.Set(PG03PolicyHeader, base64.RawURLEncoding.EncodeToString(payload))
		req.Header.Set(PG03SignatureHeader, hex.EncodeToString(mac.Sum(nil)))
		req.Header.Set(PG03RequestHeader, policy.RequestID)
	}
	// Preserve C5's independent Gateway proof and durable receipt identity.
	proofKey := os.Getenv("CLEARAPI_PG02_GATEWAY_PROOF_KEY")
	if proofKey == "" {
		return ErrPoolInvalid
	}
	proofBody, err := req.GetBody()
	if err != nil {
		return err
	}
	defer proofBody.Close()
	proof := hmac.New(sha256.New, []byte(proofKey))
	proof.Write([]byte(policy.RequestID + "\n" + policy.RequestID + "-attempt\nPOST\n/v1/responses\n"))
	if _, err := io.Copy(proof, proofBody); err != nil {
		return err
	}
	req.Header.Set("X-ClearAPI-PG02-Request", policy.RequestID)
	req.Header.Set("X-ClearAPI-PG02-Attempt", policy.RequestID+"-attempt")
	req.Header.Set("X-ClearAPI-PG02-Gateway-Proof", hex.EncodeToString(proof.Sum(nil)))
	return nil
}
