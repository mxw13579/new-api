package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInvoicePaymentEvidenceHandlerTest(t *testing.T) {
	t.Helper()
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.InvoicePaymentEvidenceBackfillRun{},
		&model.InvoicePaymentEvidenceBackfillItem{},
	))
	for _, table := range []string{"invoice_payment_evidence_backfill_items", "invoice_payment_evidence_backfill_runs"} {
		require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
	}
}

func seedInvoicePaymentEvidenceApplyingTask(t *testing.T, taskStatus model.SystemTaskStatus, lockedBy string) (*model.InvoicePaymentEvidenceBackfillRun, *model.SystemTask) {
	t.Helper()
	now := common.GetTimestamp()
	run := &model.InvoicePaymentEvidenceBackfillRun{
		PolicyVersion:           constant.InvoicePaymentEvidencePolicyVersion,
		CanonicalPolicyJSON:     constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256:            constant.InvoicePaymentEvidencePolicySHA256,
		PreviewExclusionReasons: "{}",
		Status:                  model.InvoicePaymentEvidenceRunStatusApplying,
		Attempt:                 1,
		ActorID:                 1,
		CutoverAuditJSON:        "{}",
		CreatedAt:               now,
	}
	require.NoError(t, model.DB.Create(run).Error)
	taskID, err := model.GenerateSystemTaskID()
	require.NoError(t, err)
	payload, err := common.Marshal(model.InvoicePaymentEvidenceApplyTaskPayload{
		Phase:        "apply",
		RunID:        run.ID,
		Attempt:      run.Attempt,
		PolicySHA256: run.PolicySHA256,
	})
	require.NoError(t, err)
	activeKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, run.ID)
	task := &model.SystemTask{
		TaskID:   taskID,
		Type:     constant.InvoicePaymentEvidenceTaskType,
		Status:   taskStatus,
		Payload:  string(payload),
		State:    "null",
		Result:   "null",
		LockedBy: lockedBy,
	}
	if taskStatus == model.SystemTaskStatusRunning {
		task.ActiveKey = &activeKey
	} else {
		task.Error = "lease_expired"
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Model(run).Update("active_task_id", task.TaskID).Error)
	run.ActiveTaskID = &task.TaskID
	return run, task
}

func TestInvoiceEvidencePreClaimReconcilesFailedTask(t *testing.T) {
	setupInvoicePaymentEvidenceHandlerTest(t)
	run, task := seedInvoicePaymentEvidenceApplyingTask(t, model.SystemTaskStatusFailed, "runner-old")
	handler := invoicePaymentEvidenceApplyHandler{}

	require.NoError(t, handler.ReconcileBeforeClaim(context.Background()))
	require.NoError(t, handler.ReconcileBeforeClaim(context.Background()), "reconciliation must be idempotent")

	var reloaded model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, model.DB.First(&reloaded, run.ID).Error)
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusFailed, reloaded.Status)
	assert.Nil(t, reloaded.ActiveTaskID)
	require.NotNil(t, reloaded.LastTaskID)
	assert.Equal(t, task.TaskID, *reloaded.LastTaskID)
	require.NotNil(t, reloaded.LastErrorCode)
	assert.Equal(t, "lease_expired", *reloaded.LastErrorCode)
}

func TestInvoiceEvidenceCrashBetweenExpiryAndReconcileRecovers(t *testing.T) {
	setupInvoicePaymentEvidenceHandlerTest(t)
	run, oldTask := seedInvoicePaymentEvidenceApplyingTask(t, model.SystemTaskStatusRunning, "runner-old")
	require.NoError(t, model.DB.Create(&model.SystemTaskLock{
		Type:        constant.InvoicePaymentEvidenceTaskType,
		TaskID:      oldTask.TaskID,
		LockedBy:    oldTask.LockedBy,
		LockedUntil: common.GetTimestamp() - 1,
	}).Error)

	replacementID, err := model.GenerateSystemTaskID()
	require.NoError(t, err)
	replacementKey := constant.InvoicePaymentEvidenceTaskType + ":replacement"
	replacement := &model.SystemTask{
		TaskID:    replacementID,
		Type:      constant.InvoicePaymentEvidenceTaskType,
		Status:    model.SystemTaskStatusPending,
		ActiveKey: &replacementKey,
	}
	require.NoError(t, model.DB.Create(replacement).Error)

	claimedTask, claimed, err := model.ClaimSystemTask(replacement.ID, replacement.Type, "runner-new", common.GetTimestamp()+60)
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Nil(t, claimedTask)

	var crashBoundary model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, model.DB.First(&crashBoundary, run.ID).Error)
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusApplying, crashBoundary.Status)

	handler := invoicePaymentEvidenceApplyHandler{}
	require.NoError(t, handler.ReconcileBeforeClaim(context.Background()))

	var recovered model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, model.DB.First(&recovered, run.ID).Error)
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusFailed, recovered.Status)
	assert.Nil(t, recovered.ActiveTaskID)
	var reloadedReplacement model.SystemTask
	require.NoError(t, model.DB.First(&reloadedReplacement, replacement.ID).Error)
	assert.Equal(t, model.SystemTaskStatusPending, reloadedReplacement.Status)
}

func TestInvoiceEvidenceHandlerCompletesEmptyRunAtomically(t *testing.T) {
	setupInvoicePaymentEvidenceHandlerTest(t)
	run, task := seedInvoicePaymentEvidenceApplyingTask(t, model.SystemTaskStatusRunning, "runner-live")
	require.NoError(t, model.DB.Create(&model.SystemTaskLock{
		Type:        task.Type,
		TaskID:      task.TaskID,
		LockedBy:    task.LockedBy,
		LockedUntil: common.GetTimestamp() + 60,
	}).Error)

	invoicePaymentEvidenceApplyHandler{}.Run(context.Background(), task, task.LockedBy)

	var reloadedRun model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, model.DB.First(&reloadedRun, run.ID).Error)
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusCompleted, reloadedRun.Status)
	assert.Nil(t, reloadedRun.ActiveTaskID)
	var reloadedTask model.SystemTask
	require.NoError(t, model.DB.First(&reloadedTask, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusSucceeded, reloadedTask.Status)
	assert.Nil(t, reloadedTask.ActiveKey)
}

func TestInvoiceEvidenceMalformedBoundPayloadFailsRunAndTaskAtomically(t *testing.T) {
	setupInvoicePaymentEvidenceHandlerTest(t)
	run, task := seedInvoicePaymentEvidenceApplyingTask(t, model.SystemTaskStatusRunning, "runner-invalid-payload")
	require.NoError(t, model.DB.Model(task).Update("payload", "{invalid").Error)
	task.Payload = "{invalid"
	require.NoError(t, model.DB.Create(&model.SystemTaskLock{
		Type: task.Type, TaskID: task.TaskID, LockedBy: task.LockedBy, LockedUntil: common.GetTimestamp() + 60,
	}).Error)

	invoicePaymentEvidenceApplyHandler{}.Run(context.Background(), task, task.LockedBy)

	var reloadedRun model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, model.DB.First(&reloadedRun, run.ID).Error)
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusFailed, reloadedRun.Status)
	assert.Nil(t, reloadedRun.ActiveTaskID)
	require.NotNil(t, reloadedRun.LastTaskID)
	assert.Equal(t, task.TaskID, *reloadedRun.LastTaskID)
	require.NotNil(t, reloadedRun.LastErrorCode)
	assert.Equal(t, "invalid_payload", *reloadedRun.LastErrorCode)
	var reloadedTask model.SystemTask
	require.NoError(t, model.DB.First(&reloadedTask, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusFailed, reloadedTask.Status)
	assert.Equal(t, "invalid_payload", reloadedTask.Error)
	var locks int64
	require.NoError(t, model.DB.Model(&model.SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&locks).Error)
	assert.Zero(t, locks)
	require.NoError(t, invoicePaymentEvidenceApplyHandler{}.ReconcileBeforeClaim(context.Background()))
}
