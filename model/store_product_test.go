package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateStoreProductURL(t *testing.T) {
	valid := []string{
		"https://example.com/product/100",
		"https://store.example.cn/item?sku=abc",
		"http://localhost/product/100",
		"http://127.0.0.1:8080/product/100",
		"http://[::1]:3000/product/100",
	}
	for _, raw := range valid {
		assert.NoError(t, ValidateStoreProductURL(raw), raw)
	}

	invalid := []string{
		"",
		"   ",
		"javascript:alert(1)",
		"javascript://example.com/%0aalert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"ftp://example.com/product",
		"//example.com/product/100",
		"example.com/product/100",
		"http://example.com/product/100",
		"https://",
	}
	for _, raw := range invalid {
		assert.Error(t, ValidateStoreProductURL(raw), raw)
	}
}

func setupStoreProductTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&StoreProduct{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM store_products") })
}

func TestStoreProductCRUDAndValidation(t *testing.T) {
	setupStoreProductTest(t)

	product := &StoreProduct{Name: "¥100 API 充值卡", FaceValueCents: 10000, StoreUrl: "https://example.com/product/100", Enabled: true, Sort: 3, Description: "面额 ¥100"}
	require.NoError(t, product.Insert())
	require.NotZero(t, product.Id)
	assert.Equal(t, product.CreatedTime, product.UpdatedTime)

	enabled, err := GetEnabledStoreProducts()
	require.NoError(t, err)
	require.Len(t, enabled, 1)

	disabled := &StoreProduct{Name: "¥50 卡（停用）", FaceValueCents: 5000, StoreUrl: "https://example.com/product/50", Enabled: false, Sort: 1}
	require.NoError(t, disabled.Insert())
	enabled, err = GetEnabledStoreProducts()
	require.NoError(t, err)
	require.Len(t, enabled, 1)

	all, err := GetAllStoreProducts()
	require.NoError(t, err)
	require.Len(t, all, 2)

	invalid := &StoreProduct{Name: "非法", FaceValueCents: 10000, StoreUrl: "javascript:alert(1)"}
	require.Error(t, invalid.Insert())
	invalid = &StoreProduct{Name: "零面额", FaceValueCents: 0, StoreUrl: "https://example.com/product"}
	require.Error(t, invalid.Insert())
	invalid = &StoreProduct{Name: "超画面额", FaceValueCents: maxStoreProductFaceValueCents + 1, StoreUrl: "https://example.com/product"}
	require.Error(t, invalid.Insert())
	invalid = &StoreProduct{Name: "", FaceValueCents: 10000, StoreUrl: "https://example.com/product"}
	require.Error(t, invalid.Insert())

	loaded, err := GetStoreProductById(product.Id)
	require.NoError(t, err)
	loaded.Name = "¥100 API 充值卡（改）"
	loaded.Sort = 1
	require.NoError(t, loaded.Update())
	reloaded, err := GetStoreProductById(product.Id)
	require.NoError(t, err)
	assert.Equal(t, "¥100 API 充值卡（改）", reloaded.Name)
	assert.Equal(t, 1, reloaded.Sort)
	assert.GreaterOrEqual(t, reloaded.UpdatedTime, loaded.UpdatedTime)

	require.NoError(t, DeleteStoreProductById(product.Id))
	require.Error(t, DeleteStoreProductById(product.Id))
	_, err = GetStoreProductById(product.Id)
	require.Error(t, err)
}
