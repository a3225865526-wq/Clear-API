package service

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var taskAdmission struct {
	sync.Mutex
	active int
	users  map[int]int
}

var ErrTaskAdmission = errors.New("outstanding task limit reached")

// Admission covers both outstanding persisted jobs and submissions that have
// not yet produced a task row. Production supports one submitting process.
// The overlap during insertion is deliberately conservative, never permissive.
func AdmitTaskSubmission(userID int) (func(), error) {
	if userID <= 0 || model.DB == nil {
		return nil, errors.New("task admission unavailable")
	}
	taskAdmission.Lock()
	defer taskAdmission.Unlock()
	if taskAdmission.users == nil {
		taskAdmission.users = make(map[int]int)
	}
	var total, userCount int64
	query := model.DB.Model(&model.Task{}).Where("status NOT IN ?", []model.TaskStatus{model.TaskStatusSuccess, model.TaskStatusFailure})
	if err := query.Count(&total).Error; err != nil {
		return nil, errors.New("task admission unavailable")
	}
	if err := query.Where("user_id = ?", userID).Count(&userCount).Error; err != nil {
		return nil, errors.New("task admission unavailable")
	}
	if total+int64(taskAdmission.active) >= int64(max(1, common.GetEnvOrDefault("TASK_OUTSTANDING_GLOBAL", 256))) || userCount+int64(taskAdmission.users[userID]) >= int64(max(1, common.GetEnvOrDefault("TASK_OUTSTANDING_USER", 8))) {
		return nil, ErrTaskAdmission
	}
	taskAdmission.active++
	taskAdmission.users[userID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			taskAdmission.Lock()
			defer taskAdmission.Unlock()
			taskAdmission.active--
			taskAdmission.users[userID]--
			if taskAdmission.users[userID] == 0 {
				delete(taskAdmission.users, userID)
			}
		})
	}, nil
}
