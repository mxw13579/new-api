package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type invoicePaymentEvidenceExecutionFence struct {
	run  InvoicePaymentEvidenceBackfillRun
	task SystemTask
	lock SystemTaskLock
}

func lockInvoicePaymentEvidenceExecutionFence(tx *gorm.DB, runID int64, taskID string, runnerID string, attempt int64, transactionStartedAt int64) (*invoicePaymentEvidenceExecutionFence, error) {
	if tx == nil || runID <= 0 || taskID == "" || runnerID == "" || attempt <= 0 || transactionStartedAt <= 0 {
		return nil, ErrInvoicePaymentEvidenceInvalidRequest
	}
	fence := &invoicePaymentEvidenceExecutionFence{}
	if err := lockForUpdate(tx).Where("id = ? AND status = ? AND attempt = ? AND active_task_id = ?",
		runID, InvoicePaymentEvidenceRunStatusApplying, attempt, taskID).First(&fence.run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSystemTaskLockLost
		}
		return nil, err
	}
	if err := lockInvoicePaymentEvidenceTaskFence(tx, fence, runID, taskID, runnerID, attempt, transactionStartedAt); err != nil {
		return nil, err
	}
	return fence, nil
}

func lockInvoicePaymentEvidenceTaskFence(tx *gorm.DB, fence *invoicePaymentEvidenceExecutionFence, runID int64, taskID string, runnerID string, attempt int64, transactionStartedAt int64) error {
	expectedActiveKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, runID)
	if err := lockForUpdate(tx).Where("task_id = ? AND type = ? AND status = ? AND locked_by = ?",
		taskID, constant.InvoicePaymentEvidenceTaskType, SystemTaskStatusRunning, runnerID).First(&fence.task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSystemTaskLockLost
		}
		return err
	}
	if fence.task.ActiveKey == nil || *fence.task.ActiveKey != expectedActiveKey {
		return ErrSystemTaskLockLost
	}
	var payload InvoicePaymentEvidenceApplyTaskPayload
	if common.UnmarshalJsonStr(fence.task.Payload, &payload) != nil || payload.Phase != "apply" || payload.RunID != runID ||
		payload.Attempt != attempt || payload.PolicySHA256 != fence.run.PolicySHA256 {
		return ErrSystemTaskLockLost
	}
	if err := lockForUpdate(tx).Where("type = ? AND task_id = ? AND locked_by = ?",
		constant.InvoicePaymentEvidenceTaskType, taskID, runnerID).First(&fence.lock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSystemTaskLockLost
		}
		return err
	}
	if fence.lock.LockedUntil < transactionStartedAt {
		return ErrSystemTaskLockLost
	}
	return nil
}

func RunInvoicePaymentEvidenceApplyBatch(ctx context.Context, runID int64, taskID string, runnerID string, attempt int64) (InvoicePaymentEvidenceApplyBatchResult, error) {
	if ctx == nil {
		return InvoicePaymentEvidenceApplyBatchResult{}, ErrInvoicePaymentEvidenceInvalidRequest
	}
	var result InvoicePaymentEvidenceApplyBatchResult
	err := runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
		transactionStartedAt := common.GetTimestamp()
		if _, err := lockInvoicePaymentEvidenceExecutionFence(tx, runID, taskID, runnerID, attempt, transactionStartedAt); err != nil {
			return err
		}
		var err error
		result, err = ApplyInvoicePaymentEvidenceBatchTx(tx, runID)
		return err
	})
	return result, err
}

func CompleteInvoicePaymentEvidenceApply(ctx context.Context, runID int64, taskID string, runnerID string, attempt int64) error {
	if ctx == nil {
		return ErrInvoicePaymentEvidenceInvalidRequest
	}
	return runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
		transactionStartedAt := common.GetTimestamp()
		fence, err := lockInvoicePaymentEvidenceExecutionFence(tx, runID, taskID, runnerID, attempt, transactionStartedAt)
		if err != nil {
			return err
		}
		if err := ReconcileInvoicePaymentEvidenceBackfillTx(tx, runID); err != nil {
			return err
		}
		return completeInvoicePaymentEvidenceFenceTx(tx, fence, runID, taskID, runnerID, attempt)
	})
}

func completeInvoicePaymentEvidenceFenceTx(tx *gorm.DB, fence *invoicePaymentEvidenceExecutionFence, runID int64, taskID string, runnerID string, attempt int64) error {
	now := common.GetTimestamp()
	updatedRun := tx.Model(&InvoicePaymentEvidenceBackfillRun{}).
		Where("id = ? AND status = ? AND attempt = ? AND active_task_id = ?", runID, InvoicePaymentEvidenceRunStatusApplying, attempt, taskID).
		Updates(map[string]any{
			"status": InvoicePaymentEvidenceRunStatusCompleted, "completed_at": now, "last_task_id": taskID, "active_task_id": nil,
			"failed_at": nil, "last_error_code": nil, "last_error_safe": nil,
		})
	if updatedRun.Error != nil || updatedRun.RowsAffected != 1 {
		if updatedRun.Error != nil {
			return updatedRun.Error
		}
		return ErrSystemTaskLockLost
	}
	resultJSON, err := common.Marshal(map[string]int64{"run_id": runID})
	if err != nil {
		return err
	}
	updatedTask := tx.Model(&SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ? AND active_key = ?", taskID, SystemTaskStatusRunning, runnerID, *fence.task.ActiveKey).
		Updates(map[string]any{"status": SystemTaskStatusSucceeded, "active_key": nil, "result": string(resultJSON), "error": "", "updated_at": now})
	if updatedTask.Error != nil || updatedTask.RowsAffected != 1 {
		if updatedTask.Error != nil {
			return updatedTask.Error
		}
		return ErrSystemTaskLockLost
	}
	return deleteInvoicePaymentEvidenceExecutionLock(tx, &fence.lock)
}

func FailInvoicePaymentEvidenceApply(ctx context.Context, runID int64, taskID string, runnerID string, attempt int64, safeCode string, safeMessage string) error {
	if ctx == nil || strings.TrimSpace(safeCode) == "" || strings.TrimSpace(safeMessage) == "" || len(safeCode) > 64 || len(safeMessage) > 512 {
		return ErrInvoicePaymentEvidenceInvalidRequest
	}
	return runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
		transactionStartedAt := common.GetTimestamp()
		fence, err := lockInvoicePaymentEvidenceExecutionFence(tx, runID, taskID, runnerID, attempt, transactionStartedAt)
		if err != nil {
			return err
		}
		return failInvoicePaymentEvidenceFenceTx(tx, fence, safeCode, safeMessage)
	})
}

func FailInvoicePaymentEvidenceInvalidPayload(ctx context.Context, taskID string, runnerID string) error {
	if ctx == nil || taskID == "" || runnerID == "" {
		return ErrInvoicePaymentEvidenceInvalidRequest
	}
	return runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
		fence, err := lockInvoicePaymentEvidenceInvalidPayloadFence(tx, taskID, runnerID, common.GetTimestamp())
		if err != nil {
			return err
		}
		return failInvoicePaymentEvidenceFenceTx(tx, fence, "invalid_payload", "invalid task payload")
	})
}

func lockInvoicePaymentEvidenceInvalidPayloadFence(tx *gorm.DB, taskID string, runnerID string, transactionStartedAt int64) (*invoicePaymentEvidenceExecutionFence, error) {
	fence := &invoicePaymentEvidenceExecutionFence{}
	if err := lockForUpdate(tx).Where("status = ? AND active_task_id = ?", InvoicePaymentEvidenceRunStatusApplying, taskID).First(&fence.run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSystemTaskLockLost
		}
		return nil, err
	}
	expectedActiveKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, fence.run.ID)
	if err := lockForUpdate(tx).Where("task_id = ? AND type = ? AND status = ? AND locked_by = ?", taskID,
		constant.InvoicePaymentEvidenceTaskType, SystemTaskStatusRunning, runnerID).First(&fence.task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSystemTaskLockLost
		}
		return nil, err
	}
	if fence.task.ActiveKey == nil || *fence.task.ActiveKey != expectedActiveKey {
		return nil, ErrSystemTaskLockLost
	}
	if err := lockForUpdate(tx).Where("type = ? AND task_id = ? AND locked_by = ?", constant.InvoicePaymentEvidenceTaskType,
		taskID, runnerID).First(&fence.lock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSystemTaskLockLost
		}
		return nil, err
	}
	if fence.lock.LockedUntil < transactionStartedAt {
		return nil, ErrSystemTaskLockLost
	}
	return fence, nil
}

func failInvoicePaymentEvidenceFenceTx(tx *gorm.DB, fence *invoicePaymentEvidenceExecutionFence, safeCode string, safeMessage string) error {
	now := common.GetTimestamp()
	updatedRun := tx.Model(&InvoicePaymentEvidenceBackfillRun{}).
		Where("id = ? AND status = ? AND attempt = ? AND active_task_id = ?", fence.run.ID, InvoicePaymentEvidenceRunStatusApplying, fence.run.Attempt, fence.task.TaskID).
		Updates(map[string]any{"status": InvoicePaymentEvidenceRunStatusFailed, "last_task_id": fence.task.TaskID, "active_task_id": nil,
			"failed_at": now, "last_error_code": safeCode, "last_error_safe": safeMessage})
	if updatedRun.Error != nil {
		return updatedRun.Error
	}
	if updatedRun.RowsAffected != 1 {
		return ErrSystemTaskLockLost
	}
	updatedTask := tx.Model(&SystemTask{}).
		Where("id = ? AND status = ? AND locked_by = ? AND active_key = ?", fence.task.ID, SystemTaskStatusRunning, fence.lock.LockedBy, *fence.task.ActiveKey).
		Updates(map[string]any{"status": SystemTaskStatusFailed, "active_key": nil, "error": safeCode, "updated_at": now})
	if updatedTask.Error != nil {
		return updatedTask.Error
	}
	if updatedTask.RowsAffected != 1 {
		return ErrSystemTaskLockLost
	}
	return deleteInvoicePaymentEvidenceExecutionLock(tx, &fence.lock)
}

func deleteInvoicePaymentEvidenceExecutionLock(tx *gorm.DB, lock *SystemTaskLock) error {
	deleted := tx.Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until = ?",
		lock.Type, lock.TaskID, lock.LockedBy, lock.LockedUntil).Delete(&SystemTaskLock{})
	if deleted.Error != nil {
		return deleted.Error
	}
	if deleted.RowsAffected != 1 {
		return ErrSystemTaskLockLost
	}
	return nil
}
