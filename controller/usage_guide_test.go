package controller

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service/guide"
	"github.com/gin-gonic/gin"
)

func guideTestRouter(t *testing.T, id int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("CLEARAPI_GUIDE_EDITOR_USER_ID", "5")
	t.Setenv("CLEARAPI_GUIDE_DATA_DIR", t.TempDir())
	old := usageGuide
	t.Cleanup(func() { usageGuide = old })
	if err := InitUsageGuide([]byte(`{"pages":{"/a":{"title":"A","source":"https://source.test/a","html":"<h1>A</h1>","sections":[]}}}`)); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("id", id) })
	r.GET("/access", GetUsageGuideAccess)
	r.GET("/versions", GetUsageGuideVersions)
	r.PUT("/save", SaveUsageGuide)
	r.POST("/images", UploadUsageGuideImage)
	return r
}
func TestGuideOnlyConfiguredOwnerCanEdit(t *testing.T) {
	for _, id := range []int{0, 1, 6, 5} {
		t.Run(strings.Repeat("x", id+1), func(t *testing.T) {
			r := guideTestRouter(t, id)
			access := httptest.NewRecorder()
			r.ServeHTTP(access, httptest.NewRequest("GET", "/access", nil))
			var body struct {
				Data struct {
					CanEdit bool `json:"can_edit"`
				} `json:"data"`
			}
			_ = json.Unmarshal(access.Body.Bytes(), &body)
			if body.Data.CanEdit != (id == 5) {
				t.Fatal("unexpected editor visibility")
			}
			for _, route := range []struct{ method, path string }{{"GET", "/versions?article=/a"}, {"PUT", "/save"}, {"POST", "/images"}} {
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"id":5}`))
				req.Header.Set("X-Guide-Editor", "1")
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if id != 5 && w.Code != http.StatusForbidden {
					t.Fatalf("nonowner could access %s: %d", route.path, w.Code)
				}
			}
		})
	}
}
func TestGuideWriteOriginHeaderAndConflict(t *testing.T) {
	r := guideTestRouter(t, 5)
	change := guide.Change{Path: "/a", Title: "Edited", HTML: "<h1>Edited</h1>", Catalog: []guide.Section{}}
	raw, _ := json.Marshal(change)
	cases := []struct {
		origin, header string
		want           int
	}{{"http://attacker.test", "1", 403}, {"http://example.com", "", 403}, {"http://example.com", "1", 200}, {"http://example.com", "1", 409}}
	for _, item := range cases {
		req := httptest.NewRequest("PUT", "http://example.com/save", bytes.NewReader(raw))
		req.Header.Set("Origin", item.origin)
		req.Header.Set("X-Guide-Editor", item.header)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != item.want {
			t.Fatalf("expected %d, got %d", item.want, w.Code)
		}
	}
	t.Setenv("CLEARAPI_GUIDE_EDITOR_USER_ID", "invalid")
	if canEditGuide(5) {
		t.Fatal("invalid owner config must deny")
	}
}
func TestGuideImageUploadValidatesBytes(t *testing.T) {
	r := guideTestRouter(t, 5)
	var pngBytes bytes.Buffer
	_ = png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	for _, test := range []struct {
		data []byte
		want int
	}{{[]byte("<svg onload='x'/>"), 400}, {pngBytes.Bytes(), 200}} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("image", "../../pretend.png")
		_, _ = part.Write(test.data)
		_ = writer.Close()
		req := httptest.NewRequest("POST", "/images", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("X-Guide-Editor", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Fatalf("wanted %d got %d", test.want, w.Code)
		}
	}
}
