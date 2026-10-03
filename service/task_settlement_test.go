package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestDurableTaskSettlementLifecycle(t *testing.T) {
	truncate(t)
	t.Setenv("CLEARAPI_DURABLE_WALLET", "true")
	t.Setenv("CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE", "true")
	t.Setenv("CLEARAPI_TASK_SETTLEMENT", "true")
	require.NoError(t, model.DB.AutoMigrate(&model.QuotaData{}))
	require.NoError(t, model.MigrateFinanceRequests(model.DB))
	require.NoError(t, model.MigrateBillingReconciliation(model.DB))
	require.NoError(t, model.MigrateTaskSettlements(model.DB))
	require.NoError(t, model.MigrateTaskSettlementLogs(model.LOG_DB))
	for i, immediate := range []bool{false, true} {
		id := 7600 + i
		require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("synthetic-durable-%d", id), AffCode: fmt.Sprintf("durable-aff-%d", id), Quota: 1000, Status: common.UserStatusEnabled}).Error)
		seedToken(t, id, id, fmt.Sprintf("synthetic-durable-lifecycle-%d", id), 1000)
		seedChannel(t, id)
		seedChargedAccounting(t, id, id, id, 100, 1)
		task := makeTask(id, id, 100, id, BillingSourceWallet, 0)
		requestID := fmt.Sprintf("synthetic-task-lifecycle-%d", id)
		task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: requestID}
		require.NoError(t, model.DB.Create(&model.BillingReservation{RequestID: requestID, UserID: id, TokenID: id, Reserved: 100, Applied: 100, Target: 100, State: "settled"}).Error)
		task.Status = model.TaskStatusInProgress
		if immediate {
			task.Status = model.TaskStatusFailure
		}
		require.NoError(t, task.Insert())
		if immediate {
			FinalizeSubmittedTaskBilling(context.Background(), task, &relaycommon.TaskInfo{Status: model.TaskStatusFailure})
			require.Equal(t, 1100, getUserQuota(t, id))
			require.Zero(t, getTaskQuota(t, task.ID))
		} else {
			task.Status = model.TaskStatusSuccess
			won, err := task.UpdateWithStatus(model.TaskStatusInProgress)
			require.NoError(t, err)
			require.True(t, won)
			RecalculateTaskQuota(context.Background(), task, 140, "synthetic completion")
			RecalculateTaskQuota(context.Background(), task, 140, "synthetic duplicate")
			require.Equal(t, 960, getUserQuota(t, id))
			require.Equal(t, 140, getTaskQuota(t, task.ID))
			require.Equal(t, 140, getTokenUsedQuota(t, id))
		}
		var r model.TaskSettlement
		require.NoError(t, model.DB.Where("task_id = ?", task.ID).First(&r).Error)
		require.Equal(t, "applied", r.State)
		require.Equal(t, "delivered", r.LogState)
	}
}
