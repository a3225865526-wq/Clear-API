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
	Currency         string `json:"currency"`
	Revision         int64  `json:"revision"`
	State            string `json:"state"`
	EditedBy         int    `json:"edited_by"`
	ApprovalID       int    `json:"approval_id"`
	EvidenceBatchID  int    `json:"evidence_batch_id"`
	DeliveryEvidence string `json:"delivery_evidence"`
	DeliveryBinding  string `json:"delivery_binding"`
	LegacyStoreID    int    `json:"legacy_store_id"`
	BaseQuota        int    `json:"base_quota"`
	BaseCredit       string `json:"base_credit,omitempty"`
	PurchasePath     string `json:"purchase_path,omitempty"`
	LegacyWithdrawn  bool   `json:"legacy_withdrawn"`
}

type CreditCatalog struct {
	Version        int64           `json:"version"`
	Products       []CreditProduct `json:"products"`
	ApprovalSchema int             `json:"approval_schema"`
	Unified        bool            `json:"unified"`
	// Frozen at the first local schema upgrade, never populated by an editor.
	LegacyProducts         []*StoreProduct `json:"legacy_products,omitempty"`
	LegacyCatalog          []CreditProduct `json:"legacy_catalog,omitempty"`
	LegacyEvidenceBatchIDs []int           `json:"legacy_evidence_batch_ids,omitempty"`
	// The original products key remains readable by the previous image during
	// schema preparation. New readers use this versioned authoritative list.
	ApprovalProducts []CreditProduct `json:"approval_products,omitempty"`
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
var creditProductPath = regexp.MustCompile(`^/(?:item|shop)/[a-zA-Z0-9]+$`)

func TrustedCreditPurchaseURL(raw string) bool {
	u, err := url.Parse(raw)
	return len(raw) <= 512 && err == nil && raw == "https://catfk.com"+u.Path && u.Scheme == "https" && u.Host == "catfk.com" && u.User == nil &&
		creditProductPath.MatchString(u.Path) && u.RawPath == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

func (c CreditCatalog) Validate() error {
	if c.Version <= 0 {
		return errors.New("invalid catalog version")
	}
	ids := map[string]bool{}
	for _, p := range c.Products {
		if !creditProductID.MatchString(p.ID) || ids[p.ID] {
			return errors.New("invalid or duplicate product ID")
		}
		ids[p.ID] = true
		if p.BaseQuota < 0 || (p.BaseQuota > 0 && common.ValidateWalletQuota(p.BaseQuota) != nil) {
			return errors.New("invalid base quota")
		}
		if c.ApprovalSchema != 0 && (p.Currency != "CNY" || p.Revision <= 0 || !validCreditProductState(p.State)) {
			return errors.New("invalid product revision or currency")
		}
		if c.ApprovalSchema == 1 {
			if p.State == "approved" || p.State == "published" {
				if p.ApprovalID <= 0 || !p.DeliveryVerified || p.Published != (p.State == "published") {
					return errors.New("invalid approved product declaration")
				}
			} else if p.Published || p.DeliveryVerified || p.ApprovalID != 0 {
				return errors.New("unreviewed product claims an approval")
			}
		}
		if utf8.RuneCountInString(p.Name) == 0 || utf8.RuneCountInString(p.Name) > 128 || utf8.RuneCountInString(p.Description) > 2000 {
			return errors.New("invalid product description")
		}
		if p.PriceCents <= 0 || p.PriceCents > 1000000000 || p.CreditCents < 0 || (p.CreditCents == 0 && p.BaseQuota == 0) || p.CreditCents > 1000000000 {
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
	err := DB.Where(&Option{Key: CreditCatalogOptionKey}).First(&row).Error
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
	if c.ApprovalSchema == 1 {
		c.Products = c.ApprovalProducts
	}
	sort.SliceStable(c.Products, func(i, j int) bool { return c.Products[i].Sort < c.Products[j].Sort })
	return c, c.Validate()
}

func SaveCreditCatalog(c CreditCatalog, actor int) error { return SaveCreditProductDrafts(c, actor) }
