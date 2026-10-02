package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/service/guide"
	"github.com/gin-gonic/gin"
)

var usageGuide *guide.Store

// Initialization is read-only. Missing overrides use immutable embedded content.
func InitUsageGuide(base []byte) error {
	dir := os.Getenv("CLEARAPI_GUIDE_DATA_DIR")
	if dir == "" {
		dir = "./usage-guide"
	}
	store, err := guide.NewStore(base, dir)
	if err == nil {
		usageGuide = store
	}
	return err
}
func canEditGuide(id int) bool {
	owner, err := strconv.Atoi(os.Getenv("CLEARAPI_GUIDE_EDITOR_USER_ID"))
	return err == nil && owner > 0 && id == owner
}
func guideEditor(c *gin.Context) bool {
	if !canEditGuide(c.GetInt("id")) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "GUIDE_FORBIDDEN"})
		return false
	}
	if c.Request.Method != http.MethodGet {
		origin := c.GetHeader("Origin")
		if origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != c.Request.Host || (u.Scheme != "http" && u.Scheme != "https") {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "GUIDE_ORIGIN"})
				return false
			}
		}
		if c.GetHeader("X-Guide-Editor") != "1" {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "GUIDE_HEADER"})
			return false
		}
	}
	if usageGuide == nil {
		guideFailure(c, nil)
		return false
	}
	return true
}
func guideFailure(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "GUIDE_UNAVAILABLE"
	if errors.Is(err, guide.ErrConflict) {
		status, code = http.StatusConflict, "GUIDE_CONFLICT"
	}
	if errors.Is(err, guide.ErrInvalid) {
		status, code = http.StatusBadRequest, "GUIDE_INVALID"
	}
	c.JSON(status, gin.H{"success": false, "code": code})
}
func GetUsageGuide(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if usageGuide == nil {
		guideFailure(c, nil)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": usageGuide.Read()})
}
func GetUsageGuideAccess(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"can_edit": canEditGuide(c.GetInt("id"))}})
}
func GetUsageGuideVersions(c *gin.Context) {
	if !guideEditor(c) {
		return
	}
	versions, err := usageGuide.Versions(c.Query("article"))
	if err != nil {
		guideFailure(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": versions})
}
func SaveUsageGuide(c *gin.Context) {
	if !guideEditor(c) {
		return
	}
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		guideFailure(c, guide.ErrInvalid)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024)
	var change guide.Change
	if err := c.ShouldBindJSON(&change); err != nil {
		guideFailure(c, guide.ErrInvalid)
		return
	}
	doc, err := usageGuide.Save(change, c.GetInt("id"))
	if err != nil {
		guideFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": doc})
}
func UploadUsageGuideImage(c *gin.Context) {
	if !guideEditor(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 6*1024*1024)
	file, err := c.FormFile("image")
	if err != nil || file.Size > 5*1024*1024 {
		guideFailure(c, guide.ErrInvalid)
		return
	}
	opened, err := file.Open()
	if err != nil {
		guideFailure(c, nil)
		return
	}
	defer opened.Close()
	data, err := io.ReadAll(io.LimitReader(opened, 5*1024*1024+1))
	if err != nil || len(data) > 5*1024*1024 {
		guideFailure(c, guide.ErrInvalid)
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") || config.Width <= 0 || config.Height <= 0 || config.Width > 20000 || config.Height > 20000 || int64(config.Width)*int64(config.Height) > 20000000 {
		guideFailure(c, guide.ErrInvalid)
		return
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:]) + "." + format
	if err = usageGuide.SaveImage(data, name); err != nil {
		guideFailure(c, nil)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"url": "/api/guide/images/" + name}})
}
func GetUsageGuideImage(c *gin.Context) {
	if usageGuide == nil {
		guideFailure(c, nil)
		return
	}
	file, err := usageGuide.Image(c.Param("name"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	suffix := c.Param("name")
	format := suffix[strings.LastIndex(suffix, ".")+1:]
	c.Header("Content-Type", "image/"+format)
	_, _ = io.Copy(c.Writer, file)
}
