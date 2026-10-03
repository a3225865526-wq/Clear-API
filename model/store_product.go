package model

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// StoreProduct 只是前端跳转第三方商城的档位配置；兑换金额真值永远来自
// Redemption.FaceValueCents / Redemption.Quota，绝不根据本表计算余额。
type StoreProduct struct {
	Id             int    `json:"id"`
	Name           string `json:"name" gorm:"type:varchar(64)"`
	FaceValueCents int64  `json:"face_value_cents" gorm:"type:bigint"`
	StoreUrl       string `json:"store_url" gorm:"type:varchar(512)"`
	// Enabled 不设 gorm default：避免插入 false 被 GORM 忽略而落到默认 true。
	Enabled     bool   `json:"enabled" gorm:"index:idx_store_products_enabled_sort,priority:1"`
	Sort        int    `json:"sort" gorm:"default:0;index:idx_store_products_enabled_sort,priority:2"`
	Description string `json:"description" gorm:"type:varchar(255);default:''"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

// maxStoreProductFaceValueCents 单档面额上限 ¥10,000,000.00（1e9 分），防误配。
const maxStoreProductFaceValueCents int64 = 1_000_000_000

// ValidateStoreProductURL 仅允许 https；本地调试额外允许
// http://localhost|127.0.0.1|::1。拒绝 javascript:/data:/file:/ftp:/空
// scheme/协议相对地址，必须经 net/url 解析而非前缀匹配。
func ValidateStoreProductURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("商品链接不能为空")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return errors.New("无效的商品链接")
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		host := strings.ToLower(parsed.Hostname())
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return errors.New("商品链接必须使用 https")
	default:
		return errors.New("商品链接仅支持 http(s)")
	}
}

func (product *StoreProduct) validate() error {
	if utf8.RuneCountInString(product.Name) == 0 || utf8.RuneCountInString(product.Name) > 64 {
		return errors.New("商品名称长度须为 1-64")
	}
	if product.FaceValueCents <= 0 || product.FaceValueCents > maxStoreProductFaceValueCents {
		return errors.New("商品面额超出允许范围")
	}
	if err := ValidateStoreProductURL(product.StoreUrl); err != nil {
		return err
	}
	if utf8.RuneCountInString(product.Description) > 255 {
		return errors.New("商品描述过长")
	}
	return nil
}

func (product *StoreProduct) Insert() error {
	if err := product.validate(); err != nil {
		return err
	}
	product.CreatedTime = common.GetTimestamp()
	product.UpdatedTime = product.CreatedTime
	return DB.Create(product).Error
}

func (product *StoreProduct) Update() error {
	if err := product.validate(); err != nil {
		return err
	}
	product.UpdatedTime = common.GetTimestamp()
	return DB.Model(product).Select("name", "face_value_cents", "store_url", "enabled", "sort", "description", "updated_time").
		Updates(product).Error
}

func GetStoreProductById(id int) (*StoreProduct, error) {
	if id == 0 {
		return nil, errors.New("id 为空")
	}
	var product StoreProduct
	err := DB.First(&product, "id = ?", id).Error
	return &product, err
}

func GetEnabledStoreProducts() ([]*StoreProduct, error) {
	products := []*StoreProduct{}
	err := DB.Where("enabled = ?", true).Order("sort ASC, id ASC").Find(&products).Error
	return products, err
}

func GetAllStoreProducts() ([]*StoreProduct, error) {
	products := []*StoreProduct{}
	err := DB.Order("sort ASC, id ASC").Find(&products).Error
	return products, err
}

func DeleteStoreProductById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空")
	}
	result := DB.Where("id = ?", id).Delete(&StoreProduct{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("商品不存在")
	}
	return nil
}
