package model

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type InvoicePaymentEvidenceStopResult struct {
	RunID      int64
	Status     string
	Attempt    int64
	LastTaskID *string
	FailedAt   int64
	Reason     string
}

func StopInvoicePaymentEvidenceBackfill(ctx context.Context, runID int64) (*InvoicePaymentEvidenceStopResult, error) {
	if ctx == nil || runID <= 0 {
		return nil, ErrInvoicePaymentEvidenceInvalidRequest
	}
	var result *InvoicePaymentEvidenceStopResult
	err := runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
		var err error
		result, err = stopInvoicePaymentEvidenceBackfillTx(tx, runID)
		return err
	})
	return result, err
}

func stopInvoicePaymentEvidenceBackfillTx(tx *gorm.DB, runID int64) (*InvoicePaymentEvidenceStopResult, error) {
	var run InvoicePaymentEvidenceBackfillRun
	if err := lockForUpdate(tx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoicePaymentEvidenceRunNotFound
		}
		return nil, err
	}
	if result := stoppedInvoicePaymentEvidenceResult(&run); result != nil {
		return result, nil
	}
	if run.Status != InvoicePaymentEvidenceRunStatusApplying || run.ActiveTaskID == nil {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	task, err := lockBoundInvoicePaymentEvidenceStopTask(tx, &run)
	if err != nil {
		return nil, err
	}
	lock, err := lockInvoicePaymentEvidenceStopLock(tx)
	if err != nil {
		return nil, err
	}
	if err := validateInvoicePaymentEvidenceStopFence(task, lock); err != nil {
		return nil, err
	}
	return persistInvoicePaymentEvidenceStop(tx, &run, task, lock)
}

func stoppedInvoicePaymentEvidenceResult(run *InvoicePaymentEvidenceBackfillRun) *InvoicePaymentEvidenceStopResult {
	if run.Status != InvoicePaymentEvidenceRunStatusFailed || run.LastErrorCode == nil ||
		*run.LastErrorCode != "operator_stopped" || run.FailedAt == nil {
		return nil
	}
	return &InvoicePaymentEvidenceStopResult{
		RunID: run.ID, Status: run.Status, Attempt: run.Attempt, LastTaskID: run.LastTaskID,
		FailedAt: *run.FailedAt, Reason: "operator_stopped",
	}
}

func lockBoundInvoicePaymentEvidenceStopTask(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun) (*SystemTask, error) {
	var task SystemTask
	if err := lockForUpdate(tx).Where("task_id = ?", *run.ActiveTaskID).First(&task).Error; err != nil {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	expectedActiveKey := constant.InvoicePaymentEvidenceTaskType + ":" + strconv.FormatInt(run.ID, 10)
	var payload InvoicePaymentEvidenceApplyTaskPayload
	if task.Type != constant.InvoicePaymentEvidenceTaskType || task.ActiveKey == nil || *task.ActiveKey != expectedActiveKey ||
		(task.Status != SystemTaskStatusPending && task.Status != SystemTaskStatusRunning) ||
		common.UnmarshalJsonStr(task.Payload, &payload) != nil || payload.Phase != "apply" || payload.RunID != run.ID ||
		payload.Attempt != run.Attempt || payload.PolicySHA256 != run.PolicySHA256 {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	return &task, nil
}

func lockInvoicePaymentEvidenceStopLock(tx *gorm.DB) (*SystemTaskLock, error) {
	var lock SystemTaskLock
	if err := lockForUpdate(tx).Where("type = ?", constant.InvoicePaymentEvidenceTaskType).First(&lock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &lock, nil
}

func validateInvoicePaymentEvidenceStopFence(task *SystemTask, lock *SystemTaskLock) error {
	if task.Status == SystemTaskStatusPending {
		if lock != nil || task.LockedBy != "" {
			return ErrInvoicePaymentEvidenceRunStateConflict
		}
		return nil
	}
	if lock == nil || task.LockedBy == "" || lock.TaskID != task.TaskID || lock.LockedBy != task.LockedBy ||
		lock.Type != task.Type || lock.LockedUntil <= common.GetTimestamp() {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	return nil
}

func persistInvoicePaymentEvidenceStop(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, task *SystemTask, lock *SystemTaskLock) (*InvoicePaymentEvidenceStopResult, error) {
	now := common.GetTimestamp()
	if err := failInvoicePaymentEvidenceStopTask(tx, task, now); err != nil {
		return nil, err
	}
	if err := failInvoicePaymentEvidenceStopRun(tx, run, task.TaskID, now); err != nil {
		return nil, err
	}
	if lock != nil {
		deleted := tx.Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until = ?", lock.Type, lock.TaskID, lock.LockedBy, lock.LockedUntil).Delete(&SystemTaskLock{})
		if deleted.Error != nil {
			return nil, deleted.Error
		}
		if deleted.RowsAffected != 1 {
			return nil, ErrInvoicePaymentEvidenceRunStateConflict
		}
	}
	lastTaskID := task.TaskID
	return &InvoicePaymentEvidenceStopResult{RunID: run.ID, Status: InvoicePaymentEvidenceRunStatusFailed,
		Attempt: run.Attempt, LastTaskID: &lastTaskID, FailedAt: now, Reason: "operator_stopped"}, nil
}

func failInvoicePaymentEvidenceStopTask(tx *gorm.DB, task *SystemTask, now int64) error {
	query := tx.Model(&SystemTask{}).Where("task_id = ? AND status = ? AND active_key IS NOT NULL", task.TaskID, task.Status)
	if task.Status == SystemTaskStatusRunning {
		query = query.Where("locked_by = ?", task.LockedBy)
	}
	updated := query.Updates(map[string]any{"status": SystemTaskStatusFailed, "active_key": nil, "error": "operator_stopped", "updated_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	return nil
}

func failInvoicePaymentEvidenceStopRun(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, taskID string, now int64) error {
	updated := tx.Model(&InvoicePaymentEvidenceBackfillRun{}).
		Where("id = ? AND status = ? AND attempt = ? AND active_task_id = ?", run.ID, InvoicePaymentEvidenceRunStatusApplying, run.Attempt, taskID).
		Updates(map[string]any{"status": InvoicePaymentEvidenceRunStatusFailed, "last_task_id": taskID, "active_task_id": nil,
			"failed_at": now, "last_error_code": "operator_stopped", "last_error_safe": "operator stopped"})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	return nil
}
