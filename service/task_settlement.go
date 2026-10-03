package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func settleDurableTask(ctx context.Context, task *model.Task, target int, reason string, facts map[string]any, clamps ...*common.QuotaClamp) bool {
	if task == nil || task.ID <= 0 {
		return false
	}
	encoded, err := common.Marshal(facts)
	if err != nil {
		return false
	}
	other := taskBillingOther(task)
	other.SetPublic("task_id", task.TaskID)
	other.SetPublic("reason", reason)
	other.SetPublic("actual_quota", target)
	other.SetPublic("pre_consumed_quota", task.Quota)
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	log := model.Log{ModelName: taskModelName(task), Group: task.Group, Other: other.JSONString()}
	r, err := model.FreezeTaskSettlement(model.DB.WithContext(ctx), task.ID, target, reason, string(encoded), log)
	if err != nil {
		logger.LogWarn(ctx, "task settlement final facts could not be frozen")
		return false
	}
	if _, err := model.ApplyTaskSettlement(model.DB.WithContext(ctx), r.ID); err != nil {
		logger.LogWarn(ctx, "task settlement remains unresolved; inspect owner finance")
		return false
	}
	task.Quota = target
	_ = model.DeliverTaskSettlementLogs(model.DB, model.LOG_DB)
	return true
}

// Immediate terminal results must use the same finalization path after the
// submit-time charge/statistics have been committed.
func FinalizeSubmittedTaskBilling(ctx context.Context, task *model.Task, result *relaycommon.TaskInfo) {
	if !model.TaskSettlementRequested() || result == nil {
		return
	}
	var adaptor TaskPollingAdaptor
	if GetTaskAdaptorFunc != nil {
		adaptor = GetTaskAdaptorFunc(task.Platform)
	}
	finalizeTerminalTask(ctx, adaptor, task, result)
}
