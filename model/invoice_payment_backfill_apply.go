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

type invoicePaymentEvidenceApplyResult struct {
	run     *InvoicePaymentEvidenceBackfillRun
	task    *SystemTask
	created bool
}

func validateInvoicePaymentEvidenceCutover(run *InvoicePaymentEvidenceBackfillRun) error {
	if run == nil {
		return ErrInvoicePaymentEvidenceRunNotFound
	}
	var audit InvoicePaymentEvidenceCutoverAudit
	if err := common.Unmarshal([]byte(run.CutoverAuditJSON), &audit); err != nil {
		return ErrInvoicePaymentEvidenceCutoverNotReady
	}
	if audit.SchemaVersion != 1 || !validInvoicePaymentEvidenceSHA(audit.DeploymentSHA) ||
		audit.ActiveInstanceCount <= 0 || audit.MatchingInstanceCount != audit.ActiveInstanceCount ||
		audit.DisallowedInstanceCount != 0 || audit.PostPreviewUnversionedSuccessCount != 0 {
		return ErrInvoicePaymentEvidenceCutoverNotReady
	}
	return nil
}

func EnqueueInvoicePaymentEvidenceApply(ctx context.Context, runID int64, expectedPolicySHA string) (*InvoicePaymentEvidenceBackfillRun, *SystemTask, bool, error) {
	if ctx == nil || runID <= 0 || strings.TrimSpace(expectedPolicySHA) == "" {
		return nil, nil, false, ErrInvoicePaymentEvidenceInvalidRequest
	}
	result := &invoicePaymentEvidenceApplyResult{}
	err := runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
		return enqueueInvoicePaymentEvidenceApplyTx(tx, runID, expectedPolicySHA, result)
	})
	if err != nil {
		if run, task, matched := loadCommittedInvoicePaymentEvidenceApply(ctx, runID, expectedPolicySHA); matched {
			return run, task, false, nil
		}
		return nil, nil, false, err
	}
	return result.run, result.task, result.created, nil
}

func loadCommittedInvoicePaymentEvidenceApply(ctx context.Context, runID int64, expectedPolicySHA string) (*InvoicePaymentEvidenceBackfillRun, *SystemTask, bool) {
	var run InvoicePaymentEvidenceBackfillRun
	if err := DB.WithContext(ctx).First(&run, runID).Error; err != nil || run.PolicySHA256 != expectedPolicySHA ||
		run.Status != InvoicePaymentEvidenceRunStatusApplying || run.ActiveTaskID == nil {
		return nil, nil, false
	}
	if validatePersistedInvoicePaymentEvidencePolicy(&run) != nil || validateInvoicePaymentEvidenceCutover(&run) != nil {
		return nil, nil, false
	}
	task, err := loadBoundInvoicePaymentEvidenceTask(DB.WithContext(ctx), &run)
	if err != nil {
		return nil, nil, false
	}
	return &run, task, true
}

func enqueueInvoicePaymentEvidenceApplyTx(tx *gorm.DB, runID int64, expectedPolicySHA string, result *invoicePaymentEvidenceApplyResult) error {
	run, err := loadInvoicePaymentEvidenceRunForApply(tx, runID, expectedPolicySHA)
	if err != nil {
		return err
	}
	if run.Status == InvoicePaymentEvidenceRunStatusApplying && run.ActiveTaskID != nil {
		task, err := loadBoundInvoicePaymentEvidenceTask(tx, run)
		if err != nil {
			return err
		}
		result.run, result.task = run, task
		return nil
	}
	if err := validateInvoicePaymentEvidenceApplyStart(tx, run); err != nil {
		return err
	}
	task, attempt, err := createInvoicePaymentEvidenceApplyTask(tx, run)
	if err != nil {
		return err
	}
	if err := bindInvoicePaymentEvidenceApplyRun(tx, run, task.TaskID, attempt); err != nil {
		return err
	}
	result.run, result.task, result.created = run, task, true
	return nil
}

func loadInvoicePaymentEvidenceRunForApply(tx *gorm.DB, runID int64, expectedPolicySHA string) (*InvoicePaymentEvidenceBackfillRun, error) {
	var run InvoicePaymentEvidenceBackfillRun
	if err := lockForUpdate(tx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoicePaymentEvidenceRunNotFound
		}
		return nil, err
	}
	if validatePersistedInvoicePaymentEvidencePolicy(&run) != nil || run.PolicySHA256 != expectedPolicySHA {
		return nil, ErrInvoicePaymentEvidencePolicyMismatch
	}
	if err := validateInvoicePaymentEvidenceCutover(&run); err != nil {
		return nil, err
	}
	return &run, nil
}

func loadBoundInvoicePaymentEvidenceTask(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun) (*SystemTask, error) {
	var task SystemTask
	if err := tx.Where("task_id = ?", *run.ActiveTaskID).First(&task).Error; err != nil {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	expectedActiveKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, run.ID)
	if task.Type != constant.InvoicePaymentEvidenceTaskType || task.ActiveKey == nil || *task.ActiveKey != expectedActiveKey ||
		(task.Status != SystemTaskStatusPending && task.Status != SystemTaskStatusRunning) {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	var payload InvoicePaymentEvidenceApplyTaskPayload
	if err := common.UnmarshalJsonStr(task.Payload, &payload); err != nil || payload.Phase != "apply" ||
		payload.RunID != run.ID || payload.Attempt != run.Attempt || payload.PolicySHA256 != run.PolicySHA256 {
		return nil, ErrInvoicePaymentEvidenceRunStateConflict
	}
	return &task, nil
}

func validateInvoicePaymentEvidenceApplyStart(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun) error {
	if run.ActiveTaskID != nil || (run.Status != InvoicePaymentEvidenceRunStatusPreviewed && run.Status != InvoicePaymentEvidenceRunStatusFailed) {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	if run.Status != InvoicePaymentEvidenceRunStatusFailed || run.LastTaskID == nil {
		return nil
	}
	var liveLocks int64
	if err := tx.Model(&SystemTaskLock{}).Where("type = ? AND task_id = ? AND locked_until > ?", constant.InvoicePaymentEvidenceTaskType, *run.LastTaskID, common.GetTimestamp()).Count(&liveLocks).Error; err != nil {
		return err
	}
	if liveLocks != 0 {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	return nil
}

func createInvoicePaymentEvidenceApplyTask(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun) (*SystemTask, int64, error) {
	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, 0, err
	}
	attempt := run.Attempt + 1
	payload, err := common.Marshal(InvoicePaymentEvidenceApplyTaskPayload{Phase: "apply", RunID: run.ID, Attempt: attempt, PolicySHA256: run.PolicySHA256})
	if err != nil {
		return nil, 0, err
	}
	activeKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, run.ID)
	task := &SystemTask{TaskID: taskID, Type: constant.InvoicePaymentEvidenceTaskType, Status: SystemTaskStatusPending,
		ActiveKey: &activeKey, Payload: string(payload), State: "null", Result: "null"}
	if err := tx.Create(task).Error; err != nil {
		return nil, 0, err
	}
	return task, attempt, nil
}

func bindInvoicePaymentEvidenceApplyRun(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, taskID string, attempt int64) error {
	now := common.GetTimestamp()
	update := tx.Model(&InvoicePaymentEvidenceBackfillRun{}).
		Where("id = ? AND status = ? AND attempt = ? AND active_task_id IS NULL", run.ID, run.Status, run.Attempt).
		Updates(map[string]any{"status": InvoicePaymentEvidenceRunStatusApplying, "attempt": attempt, "active_task_id": taskID,
			"applying_at": now, "failed_at": nil, "last_error_code": nil, "last_error_safe": nil})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	run.Status, run.Attempt, run.ActiveTaskID, run.ApplyingAt = InvoicePaymentEvidenceRunStatusApplying, attempt, &taskID, &now
	return nil
}
