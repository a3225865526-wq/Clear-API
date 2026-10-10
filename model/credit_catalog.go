package model

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const CreditCatalogOptionKey = "clearapi.credit_catalog"
const LegacyCreditPurchaseURL = "https://catfk.com/item/qmcse2"

type CreditProduct struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	PriceCents       int64  `json:"price_cents"`
	CreditCents      int64  `json:"credit_cents"`
	PurchaseURL      string `json:"purchase_url"`
	Published        bool   `json:"published"`
	DeliveryVerified bool   `json:"delivery_verified"`
	Sort             int    `json:"sort"`
}

type CreditCatalog struct {
	Version  int64           `json:"version"`
	Products []CreditProduct `json:"products"`
}

func DefaultCreditCatalog() CreditCatalog {
	c := CreditCatalog{Version: 1, Products: []CreditProduct{}}
	for i, face := range []int64{1, 5, 10, 20, 50, 100, 500, 1000} {
		c.Products = append(c.Products, CreditProduct{ID: fmt.Sprintf("credit-%d", face), Name: fmt.Sprintf("ClearAPI $%d API credit", face),
			Description: "Platform API usage credit. Cannot be withdrawn as cash.", PriceCents: face * 100, CreditCents: face * 100, Sort: i})
	}
	return c
}

var creditProductID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var creditProductPath = regexp.MustCompile(`^/item/[a-zA-Z0-9]+$`)

func TrustedCreditPurchaseURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "catfk.com" && u.User == nil &&
		creditProductPath.MatchString(u.Path) && u.RawPath == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

func (c CreditCatalog) Validate() error {
	if len(c.Products) == 0 || len(c.Products) > 100 {
		return errors.New("catalog must contain 1-100 products")
	}
	ids := map[string]bool{}
	for _, p := range c.Products {
		if !creditProductID.MatchString(p.ID) || ids[p.ID] {
			return errors.New("invalid or duplicate product ID")
		}
		ids[p.ID] = true
		if utf8.RuneCountInString(p.Name) == 0 || utf8.RuneCountInString(p.Name) > 128 || utf8.RuneCountInString(p.Description) > 2000 {
			return errors.New("invalid product description")
		}
		if p.PriceCents <= 0 || p.PriceCents > 1000000000 || p.CreditCents <= 0 || p.CreditCents > 1000000000 {
			return errors.New("invalid product amounts")
		}
		if p.PurchaseURL != "" && !TrustedCreditPurchaseURL(p.PurchaseURL) {
			return errors.New("invalid Catfk purchase URL")
		}
		if p.Published && (!p.DeliveryVerified || p.PurchaseURL == "") {
			return errors.New("verify delivery and purchase URL before publishing")
		}
	}
	return nil
}

func InitCreditCatalog() error {
	raw, err := common.Marshal(DefaultCreditCatalog())
	if err != nil {
		return err
	}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: CreditCatalogOptionKey, Value: string(raw)}).Error
}

func GetCreditCatalog() (CreditCatalog, error) {
	var row Option
	err := DB.Where(commonKeyCol+" = ?", CreditCatalogOptionKey).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultCreditCatalog(), nil
	}
	if err != nil {
		return CreditCatalog{}, err
	}
	var c CreditCatalog
	if err = common.UnmarshalJsonStr(row.Value, &c); err != nil {
		return c, err
	}
	sort.SliceStable(c.Products, func(i, j int) bool { return c.Products[i].Sort < c.Products[j].Sort })
	return c, c.Validate()
}

func SaveCreditCatalog(c CreditCatalog, actor int) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var row Option
		if err := lockForUpdate(tx).Where(commonKeyCol+" = ?", CreditCatalogOptionKey).First(&row).Error; err != nil {
			return err
		}
		var old CreditCatalog
		if err := common.UnmarshalJsonStr(row.Value, &old); err != nil {
			return err
		}
		if c.Version != old.Version {
			return errors.New("catalog changed; reload before saving")
		}
		c.Version++
		raw, err := common.Marshal(c)
		if err != nil {
			return err
		}
		result := tx.Model(&Option{}).Where(commonKeyCol+" = ? AND value = ?", CreditCatalogOptionKey, row.Value).Update("value", string(raw))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("catalog changed; reload before saving")
		}
		return tx.Create(&ReferralAudit{ActorID: actor, Action: "catalog", Details: string(raw), CreatedTime: common.GetTimestamp()}).Error
	})
}
