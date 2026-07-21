package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

func ReconcileInvoicePaymentEvidenceBackfills(ctx context.Context) error {
	if ctx == nil {
		return ErrInvoicePaymentEvidenceInvalidRequest
	}
	var runs []InvoicePaymentEvidenceBackfillRun
	if err := DB.WithContext(ctx).Where("status = ? AND active_task_id IS NOT NULL", InvoicePaymentEvidenceRunStatusApplying).
		Order("id asc").Find(&runs).Error; err != nil {
		return err
	}
	for i := range runs {
		runID := runs[i].ID
		if err := runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
			return reconcileInvoicePaymentEvidenceBackfillTx(tx, runID)
		}); err != nil {
			return err
		}
	}
	return nil
}

func reconcileInvoicePaymentEvidenceBackfillTx(tx *gorm.DB, runID int64) error {
	var run InvoicePaymentEvidenceBackfillRun
	if err := lockForUpdate(tx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if run.Status != InvoicePaymentEvidenceRunStatusApplying || run.ActiveTaskID == nil {
		return nil
	}
	task, err := failedInvoicePaymentEvidenceBoundTask(tx, &run)
	if err != nil || task == nil {
		return err
	}
	live, err := hasLiveInvoicePaymentEvidenceTaskLock(tx, task)
	if err != nil || live {
		return err
	}
	return failExpiredInvoicePaymentEvidenceRun(tx, &run, task.TaskID)
}

func failedInvoicePaymentEvidenceBoundTask(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun) (*SystemTask, error) {
	var task SystemTask
	if err := lockForUpdate(tx).Where("task_id = ?", *run.ActiveTaskID).First(&task).Error; err != nil {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	if task.Type != constant.InvoicePaymentEvidenceTaskType || task.Status != SystemTaskStatusFailed || task.ActiveKey != nil {
		return nil, nil
	}
	var payload InvoicePaymentEvidenceApplyTaskPayload
	if common.UnmarshalJsonStr(task.Payload, &payload) != nil || payload.Phase != "apply" || payload.RunID != run.ID ||
		payload.Attempt != run.Attempt || payload.PolicySHA256 != run.PolicySHA256 {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	return &task, nil
}

func hasLiveInvoicePaymentEvidenceTaskLock(tx *gorm.DB, task *SystemTask) (bool, error) {
	var lock SystemTaskLock
	if err := lockForUpdate(tx).Where("type = ?", constant.InvoicePaymentEvidenceTaskType).First(&lock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if lock.TaskID != task.TaskID {
		return false, ErrInvoicePaymentEvidenceRunStateConflict
	}
	return lock.LockedUntil > common.GetTimestamp(), nil
}

func failExpiredInvoicePaymentEvidenceRun(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, taskID string) error {
	now := common.GetTimestamp()
	updated := tx.Model(&InvoicePaymentEvidenceBackfillRun{}).
		Where("id = ? AND status = ? AND attempt = ? AND active_task_id = ?", run.ID, InvoicePaymentEvidenceRunStatusApplying, run.Attempt, taskID).
		Updates(map[string]any{"status": InvoicePaymentEvidenceRunStatusFailed, "last_task_id": taskID, "active_task_id": nil,
			"failed_at": now, "last_error_code": "lease_expired", "last_error_safe": "task lease expired"})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return nil
	}
	return nil
}
