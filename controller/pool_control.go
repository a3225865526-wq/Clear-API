package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/accountpool"
	"github.com/gin-gonic/gin"
)

// STEP 4 operator control plane endpoints. Every route here sits behind
// middleware.PoolOwnerAuth (root user ID 1 only); generic admins and normal
// users can neither read nor write.

// GetPoolMaster reports the runtime master switch state.
func GetPoolMaster(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"enabled": model.PoolMasterEnabled(),
	}})
}

// SetPoolMaster flips the runtime master switch. The switch is an operational
// release layer only: it never changes account trust, capability records, or
// credentials, and closing it drains new requests while in-flight requests
// finish their normal lifecycle.
func SetPoolMaster(c *gin.Context) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Enabled == nil {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	if err := model.SetPoolMasterEnabled(*input.Enabled); err != nil {
		poolError(c, err)
		return
	}
	logger.LogInfo(c, fmt.Sprintf("pool master switch set to %t by owner", *input.Enabled))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"enabled": model.PoolMasterEnabled()}})
}

// poolChannelView is one pool-bound channel projected for the control plane.
type poolChannelView struct {
	Id                 int      `json:"id"`
	Name               string   `json:"name"`
	Status             int      `json:"status"`
	PoolRoutingEnabled bool     `json:"pool_routing_enabled"`
	Models             []string `json:"models"`
	PublishedModels    []string `json:"published_models"`
}

// ListPoolChannels shows every pool-bound channel with its routing permission
// and which of its models are published for pool source. Unbound channels are
// not part of the pool control plane.
func ListPoolChannels(c *gin.Context) {
	channels, err := model.GetAllChannels(0, 0, true, true)
	if err != nil {
		poolError(c, err)
		return
	}
	views := make([]poolChannelView, 0, len(channels))
	for _, channel := range channels {
		if channel == nil || !channel.PoolChannelBound() {
			continue
		}
		view := poolChannelView{
			Id:                 channel.Id,
			Name:               channel.Name,
			Status:             channel.Status,
			PoolRoutingEnabled: channel.PoolRoutingEnabled(),
			Models:             []string{},
			PublishedModels:    []string{},
		}
		for _, name := range channel.GetModels() {
			view.Models = append(view.Models, name)
			if route, routeErr := model.GetPoolModelRoute(name); routeErr == nil && route != nil && route.Source == "pool" {
				view.PublishedModels = append(view.PublishedModels, name)
			}
		}
		views = append(views, view)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": views})
}

// SetChannelPoolRouting toggles one channel's pool routing permission. The
// write is guarded by a compare-and-set on the previous settings JSON and
// verified by a read-back before the channel cache refresh, so a failed save
// never reports success and a stale writer cannot silently overwrite the
// current configuration. Old channels without the flag never gain routing.
func SetChannelPoolRouting(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Enabled == nil {
		poolError(c, model.ErrPoolInvalid)
		return
	}
	channel, err := model.GetChannelById(id, false)
	if err != nil || channel == nil {
		poolError(c, model.ErrPoolNotFound)
		return
	}
	if !channel.PoolChannelBound() {
		poolError(c, model.ErrPoolRoutingRequiresBinder)
		return
	}
	previous := channel.OtherSettings
	var settings map[string]any
	if strings.TrimSpace(previous) != "" {
		if common.UnmarshalJsonStr(previous, &settings) != nil || settings == nil {
			settings = map[string]any{}
		}
	} else {
		settings = map[string]any{}
	}
	next := previous
	if *input.Enabled {
		settings["pool_routing_enabled"] = true
		if encoded, marshalErr := common.Marshal(settings); marshalErr == nil {
			next = string(encoded)
		} else {
			poolError(c, model.ErrPoolInvalid)
			return
		}
	} else {
		delete(settings, "pool_routing_enabled")
		if encoded, marshalErr := common.Marshal(settings); marshalErr == nil {
			next = string(encoded)
		} else {
			poolError(c, model.ErrPoolInvalid)
			return
		}
	}
	if err := model.UpdateChannelOtherSettingsCAS(id, previous, next); err != nil {
		poolError(c, err)
		return
	}
	var stored model.Channel
	if readErr := model.DB.Select("id", "settings").First(&stored, id).Error; readErr != nil || stored.OtherSettings != next {
		poolError(c, model.ErrPoolConflict)
		return
	}
	model.InitChannelCache()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"id":                  id,
		"pool_routing_enabled": *input.Enabled,
	}})
}

// GetPoolModelAvailability reports, per canonical model, how many executor
// accounts are trusted with a verified capability. This is an availability
// hint only; it never grants capability and never publishes a model.
func GetPoolModelAvailability(c *gin.Context) {
	availability, err := accountpool.PoolModelAvailability(c.Request.Context())
	if err != nil {
		poolError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": availability})
}
