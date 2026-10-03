package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// All finance endpoints (including capability) authenticate before touching
// the ledger. Phase one exposes no public download, import or pricing mutation.
func RegisterFinanceRoutes(api *gin.RouterGroup) {
	finance := api.Group("/owner/finance")
	finance.Use(middleware.DisableCache(), middleware.FinanceOwnerAuth())
	finance.GET("/capability", controller.FinanceCapability)
	finance.GET("/overview", controller.GetOwnerOverview)
	finance.GET("/profit-summary", controller.GetFinanceProfitSummary)
	finance.POST("/revenue/preview", controller.PreviewFinanceRevenue)
	finance.POST("/wallets/:user_id/events", controller.CreateFinanceRevenue)
	finance.GET("/wallets/:user_id/events", controller.ListFinanceRevenue)
	finance.GET("/wallets/:user_id/summary", controller.GetFinanceWalletSummary)
	finance.GET("/wallets/:user_id/allocations", controller.ListFinanceRevenueAllocations)
	finance.GET("/request-events", controller.ListFinanceRequestEvents)
	finance.GET("/capture-health", controller.GetFinanceCaptureHealth)
	finance.GET("/sync-health", controller.GetFinanceSyncHealth)
	finance.GET("/sync-events", controller.ListFinanceSyncEvents)
	finance.GET("/pool-cost-summary", controller.GetFinancePoolCostSummary)
	finance.POST("/supplier-bills", controller.CreateFinanceSupplierBill)
	finance.GET("/supplier-bills", controller.ListFinanceSupplierBills)
	finance.GET("/supplier-bills/reconciliation", controller.GetFinanceSupplierReconciliation)
	finance.POST("/supplier-bill-lines", controller.CreateFinanceSupplierBillLine)
	finance.GET("/supplier-bill-lines", controller.ListFinanceSupplierBillLines)
	finance.GET("/supplier-bill-lines/summary", controller.GetFinanceSupplierLinesSummary)
	finance.POST("/cash-receipts", controller.ConfirmFinanceCashReceipt)
	finance.GET("/cash-receipts", controller.ListFinanceCashReceipts)
	finance.GET("/cash-receipts/summary", controller.GetFinanceCashSummary)
	finance.POST("/cash-refunds", controller.CreateFinanceCashRefund)
	finance.GET("/cash-refunds", controller.ListFinanceCashRefunds)
	finance.POST("/wallets/:user_id/offline-sources/preview", controller.PreviewFinanceOfflineSources)
	finance.POST("/wallets/:user_id/offline-sources/import", controller.ImportFinanceOfflineSources)
	finance.POST("/wallets/:user_id/consumptions/preview", controller.PreviewFinanceConsumptions)
	finance.POST("/wallets/:user_id/consumptions/import", controller.ImportFinanceConsumptions)
	finance.GET("/wallets/:user_id/balance-check", controller.GetFinanceBalanceCheck)
	finance.POST("/wallets/:user_id/rebuild/preview", controller.PreviewFinanceWalletRebuild)
	finance.POST("/wallets/:user_id/rebuild/apply", controller.ApplyFinanceWalletRebuild)
	finance.GET("/versions/:kind", controller.ListFinanceVersions)
	finance.POST("/versions/:kind", controller.CreateFinanceVersion)
	finance.GET("/expenses", controller.ListFinanceExpenses)
	finance.POST("/expenses", controller.CreateFinanceExpense)
	finance.GET("/expenses/summary", controller.GetFinanceExpenseSummary)
	finance.GET("/audit", controller.ListFinanceAudit)
}
