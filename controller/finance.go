package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type financeVersionRequest struct {
	RecordKey     string          `json:"record_key"`
	ExpectedID    int64           `json:"expected_id"`
	EffectiveFrom int64           `json:"effective_from"`
	EffectiveTo   int64           `json:"effective_to"`
	Active        bool            `json:"active"`
	Source        string          `json:"source"`
	Payload       json.RawMessage `json:"payload"`
}

type financeVersionView struct {
	model.FinanceVersion
	Payload json.RawMessage `json:"payload"`
}

func financePage(c *gin.Context) (*common.PageInfo, error) {
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.Page > 10000000 || page.PageSize < 1 || page.PageSize > 100 {
		return nil, model.ErrFinanceInvalid
	}
	return page, nil
}

func financeError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "FINANCE_UNAVAILABLE"
	if errors.Is(err, model.ErrFinanceInvalid) {
		status, code = http.StatusBadRequest, "FINANCE_INVALID"
	}
	if errors.Is(err, model.ErrFinanceConflict) {
		status, code = http.StatusConflict, "FINANCE_CONFLICT"
	}
	// Never serialize SQL, DSNs, rejected payloads or database driver errors.
	c.JSON(status, gin.H{"success": false, "code": code})
}

func FinanceCapability(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"can_access": true, "currency": "CNY", "timezone": "Asia/Shanghai", "allocation": "fifo", "package_value": "total_quota", "stage": 10, "revenue_preview_available": true, "schema_ready": model.FinanceReady(model.DB) == nil, "source_ledger_ready": model.FinanceRevenueReady(model.DB) == nil, "source_ledger_coverage": "partial_confirmed_facts", "request_capture_ready": model.FinanceRequestsReady(model.DB) == nil, "cash_receipts_ready": model.FinanceReceiptsReady(model.DB) == nil, "cash_refunds_ready": model.FinanceRefundsReady(model.DB) == nil, "supplier_bills_ready": model.FinanceSupplierReady(model.DB) == nil, "supplier_bill_lines_ready": model.FinanceSupplierLinesReady(model.DB) == nil, "sync_events_ready": model.FinanceSyncReady(model.DB) == nil, "rebuilds_ready": model.FinanceRebuildsReady(model.DB) == nil, "pool_cost_summary_available": true}})
}

func GetFinancePoolCostSummary(c *gin.Context) {
	from, errFrom := strconv.ParseInt(c.Query("from"), 10, 64)
	to, errTo := strconv.ParseInt(c.Query("to"), 10, 64)
	if errFrom != nil || errTo != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.SummarizeFinancePoolCosts(model.DB, from, to)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func CreateFinanceSupplierBill(c *gin.Context) {
	var input service.FinanceSupplierInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	entry, err := service.RegisterFinanceSupplierBill(model.DB, input, c.GetInt("id"), c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": entry})
}

func CreateFinanceSupplierBillLine(c *gin.Context) {
	var input service.FinanceSupplierLineInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	entry, err := service.RegisterFinanceSupplierBillLine(model.DB, input, c.GetInt("id"), c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": entry})
}

func ListFinanceSupplierBillLines(c *gin.Context) {
	billID, err := strconv.ParseInt(c.Query("bill_id"), 10, 64)
	if err != nil || billID <= 0 {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	lines, bill, err := service.ListFinanceSupplierBillLines(model.DB, billID)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"bill": bill, "items": lines}})
}

func GetFinanceSupplierLinesSummary(c *gin.Context) {
	billID, err := strconv.ParseInt(c.Query("bill_id"), 10, 64)
	if err != nil || billID <= 0 {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.SummarizeFinanceSupplierLines(model.DB, billID)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func ListFinanceSupplierBills(c *gin.Context) {
	if err := model.FinanceSupplierReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	channelID, err := strconv.Atoi(c.Query("channel_id"))
	if err != nil || channelID <= 0 {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	query := model.DB.Model(&model.FinanceSupplierBill{}).Where("channel_id = ?", channelID)
	var total int64
	var entries []model.FinanceSupplierBill
	if err := query.Count(&total).Error; err != nil {
		financeError(c, err)
		return
	}
	if err := query.Order("revision desc").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": entries, "total": total}})
}

func GetFinanceSupplierReconciliation(c *gin.Context) {
	channelID, errChannel := strconv.Atoi(c.Query("channel_id"))
	from, errFrom := strconv.ParseInt(c.Query("from"), 10, 64)
	to, errTo := strconv.ParseInt(c.Query("to"), 10, 64)
	if errChannel != nil || errFrom != nil || errTo != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.ReconcileFinanceSupplier(model.DB, channelID, from, to)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func PreviewFinanceConsumptions(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var input service.FinanceConsumptionImport
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.PreviewFinanceConsumptions(model.DB, userID, input.ObservationIDs)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"sources": result, "status": "partial", "warnings": []string{"captured_settlements_only", "late_refunds_require_review", "historical_opening_and_balance_not_reconciled"}}})
}

func ImportFinanceConsumptions(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var input service.FinanceConsumptionImport
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.ImportFinanceConsumptions(model.DB, userID, c.GetInt("id"), input, c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"events": result, "status": "partial", "business_balance_unchanged": true}})
}

func GetFinanceBalanceCheck(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	result, err := service.CheckFinanceBalance(model.DB, userID)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func ConfirmFinanceCashReceipt(c *gin.Context) {
	var input service.FinanceReceiptConfirmation
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.ConfirmFinanceOfflineReceipt(model.DB, c.GetInt("id"), input, c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result, "verification": "owner_confirmed_manual"})
}

func ListFinanceCashReceipts(c *gin.Context) {
	if err := model.FinanceReceiptsReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var total int64
	if err := model.DB.Model(&model.FinanceCashReceipt{}).Count(&total).Error; err != nil {
		financeError(c, err)
		return
	}
	var entries []model.FinanceCashReceipt
	if err := model.DB.Order("id desc").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": entries, "total": total, "status": "partial", "verification": "owner_confirmed_manual"}})
}

func CreateFinanceCashRefund(c *gin.Context) {
	var input service.FinanceCashRefundInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.RegisterFinanceCashRefund(model.DB, c.GetInt("id"), input, c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result, "verification": "owner_confirmed_manual"})
}

func ListFinanceCashRefunds(c *gin.Context) {
	if err := model.FinanceRefundsReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var total int64
	if err := model.DB.Model(&model.FinanceCashRefund{}).Count(&total).Error; err != nil {
		financeError(c, err)
		return
	}
	var entries []model.FinanceCashRefund
	if err := model.DB.Order("id desc").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": entries, "total": total, "status": "partial", "verification": "owner_confirmed_manual"}})
}

func GetFinanceCashSummary(c *gin.Context) {
	from, errFrom := strconv.ParseInt(c.Query("from"), 10, 64)
	to, errTo := strconv.ParseInt(c.Query("to"), 10, 64)
	if errFrom != nil || errTo != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.SummarizeFinanceCash(model.DB, from, to)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func PreviewFinanceOfflineSources(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var input service.FinanceOfflineImport
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.PreviewFinanceOfflineSources(model.DB, userID, input.RedemptionIDs)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"sources": result, "status": "partial", "warnings": []string{"cash_confirmation_required_for_import", "historical_opening_balance_not_reconstructed", "business_balance_not_reconciled"}}})
}

func ImportFinanceOfflineSources(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var input service.FinanceOfflineImport
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.ImportFinanceOfflineSources(model.DB, userID, c.GetInt("id"), input, c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"events": result, "status": "partial", "cash_recorded_again": false, "business_balance_not_reconciled": true}})
}

// A bounded, stateless preview. It deliberately accepts no user ID, queries no
// user data, writes no ledger/audit rows, and returns no actual-profit claim.
func PreviewFinanceRevenue(c *gin.Context) {
	var input service.FinanceRevenuePreview
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 131072)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.PreviewFinanceRevenue(input)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func financeWalletUser(c *gin.Context) (int, error) {
	id, err := strconv.ParseInt(c.Param("user_id"), 10, 32)
	if err != nil || id <= 0 {
		return 0, model.ErrFinanceInvalid
	}
	return int(id), nil
}

func CreateFinanceRevenue(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var input service.FinanceRevenueWrite
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	entry, err := service.AppendFinanceRevenue(model.DB, userID, c.GetInt("id"), input, c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": entry})
}

func GetFinanceWalletSummary(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	result, err := service.SummarizeFinanceWallet(model.DB, userID)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func ListFinanceRevenue(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	if err := model.FinanceRevenueReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	query := model.DB.Model(&model.FinanceRevenueRecord{}).Where("user_id = ?", userID)
	var total int64
	var entries []model.FinanceRevenueRecord
	if err := query.Count(&total).Error; err != nil {
		financeError(c, err)
		return
	}
	if err := query.Order("revision desc").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": entries, "total": total}})
}

func ListFinanceRevenueAllocations(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	if err := model.FinanceRevenueReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	eventID, err := strconv.ParseInt(c.Query("event_id"), 10, 64)
	if err != nil || eventID <= 0 {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	var record model.FinanceRevenueRecord
	if err := model.DB.Where("id = ? AND user_id = ?", eventID, userID).First(&record).Error; err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	query := model.DB.Model(&model.FinanceRevenueAllocation{}).Where("event_id = ?", record.ID)
	var total int64
	var entries []model.FinanceRevenueAllocation
	if err := query.Count(&total).Error; err != nil {
		financeError(c, err)
		return
	}
	if err := query.Order("id asc").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": entries, "total": total}})
}

func GetFinanceCaptureHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": service.FinanceCaptureHealth()})
}

func PreviewFinanceWalletRebuild(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var input service.FinanceRebuildInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.PreviewFinanceWalletRebuild(model.DB, userID, input.SyncEventIDs)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func ApplyFinanceWalletRebuild(c *gin.Context) {
	userID, err := financeWalletUser(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var input service.FinanceRebuildInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.ApplyFinanceWalletRebuild(model.DB, userID, c.GetInt("id"), input, c.GetString(common.RequestIdKey))
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result, "rebuild": "owner_initiated_controlled"})
}

func GetFinanceSyncHealth(c *gin.Context) {
	result, err := service.FinanceSyncHealth(model.DB)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func ListFinanceRequestEvents(c *gin.Context) {
	if err := model.FinanceRequestsReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	query := model.DB.Model(&model.FinanceRequestEvent{})
	if requestID := c.Query("request_id"); requestID != "" {
		if len(requestID) > 64 {
			financeError(c, model.ErrFinanceInvalid)
			return
		}
		query = query.Where("request_id = ?", requestID)
	}
	var count int64
	var entries []model.FinanceRequestEvent
	if err := query.Count(&count).Error; err != nil {
		financeError(c, err)
		return
	}
	if err := query.Order("id desc").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	type eventView struct {
		model.FinanceRequestEvent
		Payload json.RawMessage `json:"payload"`
	}
	views := make([]eventView, 0, len(entries))
	for _, entry := range entries {
		views = append(views, eventView{FinanceRequestEvent: entry, Payload: json.RawMessage(entry.Payload)})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": views, "total": count}})
}

func ListFinanceVersions(c *gin.Context) {
	kind := c.Param("kind")
	if err := service.ValidateFinanceKind(kind); err != nil {
		financeError(c, err)
		return
	}
	if err := model.FinanceReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	query := model.DB.Model(&model.FinanceVersion{}).Where("kind = ?", kind)
	if key := c.Query("record_key"); key != "" {
		query = query.Where("record_key = ?", key)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		financeError(c, err)
		return
	}
	var entries []model.FinanceVersion
	if err := query.Order("id desc").Offset(page.GetStartIdx()).Limit(min(page.GetPageSize(), 100)).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	views := make([]financeVersionView, 0, len(entries))
	for _, entry := range entries {
		views = append(views, financeVersionView{FinanceVersion: entry, Payload: json.RawMessage(entry.Payload)})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": views, "total": total}})
}

func CreateFinanceVersion(c *gin.Context) {
	var request financeVersionRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.ExpectedID < 0 {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	entry := model.FinanceVersion{Kind: c.Param("kind"), RecordKey: request.RecordKey, EffectiveFrom: request.EffectiveFrom, EffectiveTo: request.EffectiveTo, Active: request.Active, Source: request.Source, CreatedBy: c.GetInt("id")}
	if err := service.ValidateFinanceVersion(&entry, request.Payload); err != nil {
		financeError(c, err)
		return
	}
	if err := model.AppendFinanceVersion(model.DB, &entry, request.ExpectedID, c.GetString(common.RequestIdKey)); err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": financeVersionView{FinanceVersion: entry, Payload: json.RawMessage(entry.Payload)}})
}

func ListFinanceExpenses(c *gin.Context) {
	if err := model.FinanceReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var entries []model.FinanceExpense
	var total int64
	if err := model.DB.Model(&model.FinanceExpense{}).Count(&total).Error; err != nil {
		financeError(c, err)
		return
	}
	if err := model.DB.Order("id desc").Offset(page.GetStartIdx()).Limit(min(page.GetPageSize(), 100)).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": entries, "total": total}})
}

func CreateFinanceExpense(c *gin.Context) {
	if err := model.FinanceReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	var entry model.FinanceExpense
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &entry); err != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	entry.ID, entry.CreatedAt, entry.CreatedBy = 0, 0, c.GetInt("id")
	if err := service.ValidateFinanceExpense(model.DB, &entry); err != nil {
		financeError(c, err)
		return
	}
	if err := model.AppendFinanceExpense(model.DB, &entry, c.GetString(common.RequestIdKey)); err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": entry})
}

func GetFinanceExpenseSummary(c *gin.Context) {
	from, errFrom := strconv.ParseInt(c.Query("from"), 10, 64)
	to, errTo := strconv.ParseInt(c.Query("to"), 10, 64)
	if errFrom != nil || errTo != nil {
		financeError(c, model.ErrFinanceInvalid)
		return
	}
	result, err := service.SummarizeFinanceExpenses(model.DB, from, to)
	if err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func ListFinanceAudit(c *gin.Context) {
	if err := model.FinanceReady(model.DB); err != nil {
		financeError(c, err)
		return
	}
	page, err := financePage(c)
	if err != nil {
		financeError(c, err)
		return
	}
	var entries []model.FinanceAudit
	if err := model.DB.Order("id desc").Offset(page.GetStartIdx()).Limit(min(page.GetPageSize(), 100)).Find(&entries).Error; err != nil {
		financeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entries})
}
