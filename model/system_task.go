package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type SystemTaskStatus string

const (
	SystemTaskStatusPending   SystemTaskStatus = "pending"
	SystemTaskStatusRunning   SystemTaskStatus = "running"
	SystemTaskStatusSucceeded SystemTaskStatus = "succeeded"
	SystemTaskStatusFailed    SystemTaskStatus = "failed"

	SystemTaskTypeLogCleanup     = "log_cleanup"
	SystemTaskTypeChannelTest    = "channel_test"
	SystemTaskTypeModelUpdate    = "model_update"
	SystemTaskTypeMidjourneyPoll = "midjourney_poll"
	SystemTaskTypeAsyncTaskPoll  = "async_task_poll"
)

var ErrSystemTaskLockLost = errors.New("system task lock lost")

type SystemTask struct {
	ID        int64            `json:"id" gorm:"primary_key"`
	TaskID    string           `json:"task_id" gorm:"type:varchar(64);uniqueIndex"`
	Type      string           `json:"type" gorm:"type:varchar(64);index"`
	Status    SystemTaskStatus `json:"status" gorm:"type:varchar(32);index"`
	ActiveKey *string          `json:"active_key,omitempty" gorm:"type:varchar(64);uniqueIndex"`
	Payload   string           `json:"payload" gorm:"type:text"`
	State     string           `json:"state" gorm:"type:text"`
	Result    string           `json:"result" gorm:"type:text"`
	Error     string           `json:"error" gorm:"type:text"`
	LockedBy  string           `json:"locked_by" gorm:"type:varchar(128);index"`
	CreatedAt int64            `json:"created_at" gorm:"bigint;index"`
	UpdatedAt int64            `json:"updated_at" gorm:"bigint;index"`
}

type SystemTaskLock struct {
	Type        string `json:"type" gorm:"type:varchar(64);primaryKey"`
	TaskID      string `json:"task_id" gorm:"type:varchar(64);index"`
	LockedBy    string `json:"locked_by" gorm:"type:varchar(128);index"`
	LockedUntil int64  `json:"locked_until" gorm:"bigint;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;index"`
}

type SystemTaskResponse struct {
	ID        int64            `json:"id"`
	TaskID    string           `json:"task_id"`
	Type      string           `json:"type"`
	Status    SystemTaskStatus `json:"status"`
	ActiveKey *string          `json:"active_key,omitempty"`
	Payload   any              `json:"payload"`
	State     any              `json:"state"`
	Result    any              `json:"result"`
	Error     string           `json:"error"`
	LockedBy  string           `json:"locked_by"`
	CreatedAt int64            `json:"created_at"`
	UpdatedAt int64            `json:"updated_at"`
}

func (task *SystemTask) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = now
	}
	return nil
}

func (lock *SystemTaskLock) BeforeCreate(_ *gorm.DB) error {
	if lock.UpdatedAt == 0 {
		lock.UpdatedAt = common.GetTimestamp()
	}
	return nil
}

func GenerateSystemTaskID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return "systask_" + key, nil
}

func CreateSystemTask(taskType string, payload any, state any) (*SystemTask, error) {
	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, err
	}
	payloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, err
	}
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return nil, err
	}

	task := &SystemTask{
		TaskID:    taskID,
		Type:      taskType,
		Status:    SystemTaskStatusPending,
		ActiveKey: &taskType,
		Payload:   payloadText,
		State:     stateText,
	}

	if err := DB.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

func GetSystemTaskByTaskID(taskID string) (*SystemTask, error) {
	var task SystemTask
	if err := DB.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetActiveSystemTask(taskType string) (*SystemTask, error) {
	var task SystemTask
	err := DB.Where("type = ? AND status IN ?", taskType, activeSystemTaskStatuses()).
		Order("id desc").
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func FindPendingSystemTasks(taskType string, limit int) ([]*SystemTask, error) {
	var tasks []*SystemTask
	if limit <= 0 {
		limit = 1
	}
	err := DB.Where("type = ? AND status = ?", taskType, SystemTaskStatusPending).
		Order("id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func FindEarliestPendingSystemTasks(taskTypes []string) (map[string]*SystemTask, error) {
	tasksByType := map[string]*SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := DB.Model(&SystemTask{}).
		Select("MIN(id)").
		Where("type IN ? AND status = ?", taskTypes, SystemTaskStatusPending).
		Group("type")
	var tasks []*SystemTask
	if err := DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func ListSystemTasks(limit int) ([]*SystemTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var tasks []*SystemTask
	err := DB.Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetLatestSystemTask returns the most recent task row of the given type
// (any status) so the scheduler can decide whether enough time has elapsed
// since the last run. Returns (nil, nil) when no row exists.
func GetLatestSystemTask(taskType string) (*SystemTask, error) {
	var task SystemTask
	err := DB.Where("type = ?", taskType).Order("id desc").First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetLatestSystemTasks(taskTypes []string) (map[string]*SystemTask, error) {
	tasksByType := map[string]*SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := DB.Model(&SystemTask{}).
		Select("MAX(id)").
		Where("type IN ?", taskTypes).
		Group("type")
	var tasks []*SystemTask
	if err := DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func ClaimSystemTask(id int64, taskType string, runnerID string, lockUntil int64) (*SystemTask, bool, error) {
	now := common.GetTimestamp()
	if id <= 0 || taskType == "" || runnerID == "" || lockUntil <= now {
		return nil, false, nil
	}
	var claimedTask *SystemTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		blocked, err := retireSystemTaskLockBeforeClaimTx(tx, taskType, now)
		if err != nil || blocked {
			return err
		}
		claimedTask, err = claimPendingSystemTaskTx(tx, id, taskType, runnerID, now, lockUntil)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return claimedTask, claimedTask != nil, nil
}

func retireSystemTaskLockBeforeClaimTx(tx *gorm.DB, taskType string, now int64) (bool, error) {
	var existing SystemTaskLock
	if err := tx.Where("type = ?", taskType).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if existing.LockedUntil >= now {
		return true, nil
	}
	_, err := expireExactSystemTaskLockTx(tx, &existing, now)
	return true, err
}

func claimPendingSystemTaskTx(tx *gorm.DB, id int64, taskType string, runnerID string, now int64, lockUntil int64) (*SystemTask, error) {
	var task SystemTask
	if err := lockForUpdate(tx).Where("id = ? AND type = ? AND status = ?", id, taskType, SystemTaskStatusPending).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	lock := &SystemTaskLock{Type: taskType, TaskID: task.TaskID, LockedBy: runnerID, LockedUntil: lockUntil, UpdatedAt: now}
	if err := tx.Create(lock).Error; err != nil {
		return nil, err
	}
	updated := tx.Model(&SystemTask{}).
		Where("id = ? AND type = ? AND status = ?", id, taskType, SystemTaskStatusPending).
		Updates(map[string]any{"status": SystemTaskStatusRunning, "locked_by": runnerID, "updated_at": now})
	if updated.Error != nil {
		return nil, updated.Error
	}
	if updated.RowsAffected != 1 {
		return nil, ErrSystemTaskLockLost
	}
	if err := tx.First(&task, id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func UpdateSystemTaskState(taskID string, lockedBy string, state any) error {
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := DB.Model(&SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, SystemTaskStatusRunning, lockedBy).
		Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
		Updates(map[string]any{
			"state":      stateText,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return nil
}

func RenewSystemTaskLock(taskID string, lockedBy string, lockUntil int64) error {
	now := common.GetTimestamp()
	if taskID == "" || lockedBy == "" || lockUntil <= now {
		return ErrSystemTaskLockLost
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		_, lock, err := lockRunningSystemTaskFenceTx(tx, taskID, lockedBy, now)
		if err != nil {
			return err
		}
		updated := tx.Model(&SystemTaskLock{}).
			Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until = ?", lock.Type, lock.TaskID, lock.LockedBy, lock.LockedUntil).
			Updates(map[string]any{"locked_until": lockUntil, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrSystemTaskLockLost
		}
		return nil
	})
}

func ExpireStaleSystemTaskLocks(now int64) error {
	var locks []*SystemTaskLock
	if err := DB.Where("locked_until < ?", now).Find(&locks).Error; err != nil {
		return err
	}
	for _, lock := range locks {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			_, err := expireExactSystemTaskLockTx(tx, lock, now)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func expireExactSystemTaskLockTx(tx *gorm.DB, snapshot *SystemTaskLock, now int64) (bool, error) {
	if tx == nil || snapshot == nil {
		return false, ErrSystemTaskLockLost
	}
	var task SystemTask
	if err := lockForUpdate(tx).Where("task_id = ? AND type = ?", snapshot.TaskID, snapshot.Type).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	var lock SystemTaskLock
	if err := lockForUpdate(tx).Where("type = ?", snapshot.Type).First(&lock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if lock.TaskID != snapshot.TaskID || lock.LockedBy != snapshot.LockedBy || lock.LockedUntil != snapshot.LockedUntil || lock.LockedUntil >= now {
		return false, nil
	}
	return expireSystemTaskOwnerTx(tx, &task, &lock, now)
}

func expireSystemTaskOwnerTx(tx *gorm.DB, task *SystemTask, lock *SystemTaskLock, now int64) (bool, error) {
	if task.LockedBy != lock.LockedBy {
		return false, nil
	}
	if task.Status == SystemTaskStatusRunning {
		updated := tx.Model(&SystemTask{}).
			Where("id = ? AND status = ? AND locked_by = ?", task.ID, SystemTaskStatusRunning, lock.LockedBy).
			Updates(map[string]any{"status": SystemTaskStatusFailed, "active_key": nil, "error": "lease_expired", "updated_at": now})
		if updated.Error != nil {
			return false, updated.Error
		}
		if updated.RowsAffected != 1 {
			return false, ErrSystemTaskLockLost
		}
	} else if task.Status != SystemTaskStatusSucceeded && task.Status != SystemTaskStatusFailed {
		return false, nil
	}
	deleted := tx.Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until = ?", lock.Type, lock.TaskID, lock.LockedBy, lock.LockedUntil).
		Delete(&SystemTaskLock{})
	if deleted.Error != nil {
		return false, deleted.Error
	}
	if deleted.RowsAffected != 1 {
		return false, ErrSystemTaskLockLost
	}
	return true, nil
}

func ReleaseSystemTaskLock(taskID string, lockedBy string) error {
	result := DB.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).Delete(&SystemTaskLock{})
	return result.Error
}

func FinishSystemTask(taskID string, lockedBy string, status SystemTaskStatus, resultPayload any, errorMessage string) error {
	resultText, err := marshalSystemTaskJSON(resultPayload)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()
		task, lock, err := lockRunningSystemTaskFenceTx(tx, taskID, lockedBy, now)
		if err != nil {
			return err
		}
		return finishSystemTaskTx(tx, task, lock, status, resultText, errorMessage, now)
	})
}

func lockRunningSystemTaskFenceTx(tx *gorm.DB, taskID string, lockedBy string, now int64) (*SystemTask, *SystemTaskLock, error) {
	var task SystemTask
	if err := lockForUpdate(tx).Where("task_id = ? AND status = ? AND locked_by = ?", taskID, SystemTaskStatusRunning, lockedBy).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrSystemTaskLockLost
		}
		return nil, nil, err
	}
	var lock SystemTaskLock
	if err := lockForUpdate(tx).Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until >= ?", task.Type, taskID, lockedBy, now).First(&lock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrSystemTaskLockLost
		}
		return nil, nil, err
	}
	return &task, &lock, nil
}

func finishSystemTaskTx(tx *gorm.DB, task *SystemTask, lock *SystemTaskLock, status SystemTaskStatus, resultText string, errorMessage string, now int64) error {
	updated := tx.Model(&SystemTask{}).Where("id = ? AND status = ? AND locked_by = ?", task.ID, SystemTaskStatusRunning, lock.LockedBy).
		Updates(map[string]any{"status": status, "active_key": nil, "result": resultText, "error": errorMessage, "updated_at": now})
	if updated.Error != nil || updated.RowsAffected != 1 {
		if updated.Error != nil {
			return updated.Error
		}
		return ErrSystemTaskLockLost
	}
	deleted := tx.Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until = ?", lock.Type, lock.TaskID, lock.LockedBy, lock.LockedUntil).
		Delete(&SystemTaskLock{})
	if deleted.Error != nil || deleted.RowsAffected != 1 {
		if deleted.Error != nil {
			return deleted.Error
		}
		return ErrSystemTaskLockLost
	}
	return nil
}

func (task *SystemTask) DecodePayload(v any) error {
	return decodeSystemTaskJSONString(task.Payload, v)
}

func (task *SystemTask) DecodeState(v any) error {
	return decodeSystemTaskJSONString(task.State, v)
}

func (task *SystemTask) ToResponse() SystemTaskResponse {
	return SystemTaskResponse{
		ID:        task.ID,
		TaskID:    task.TaskID,
		Type:      task.Type,
		Status:    task.Status,
		ActiveKey: task.ActiveKey,
		Payload:   decodeSystemTaskJSONValue(task.Payload),
		State:     decodeSystemTaskJSONValue(task.State),
		Result:    decodeSystemTaskJSONValue(task.Result),
		Error:     task.Error,
		LockedBy:  task.LockedBy,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}
}

func activeSystemTaskStatuses() []string {
	return []string{string(SystemTaskStatusPending), string(SystemTaskStatusRunning)}
}

func marshalSystemTaskJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSystemTaskJSONString(data string, v any) error {
	if data == "" {
		return nil
	}
	return common.UnmarshalJsonStr(data, v)
}

func decodeSystemTaskJSONValue(data string) any {
	if data == "" {
		return nil
	}
	var value any
	if err := common.UnmarshalJsonStr(data, &value); err != nil {
		return data
	}
	return value
}
