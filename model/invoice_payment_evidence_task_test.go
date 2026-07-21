package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedFencedInvoicePaymentEvidenceTask(t *testing.T) (*InvoicePaymentEvidenceBackfillRun, *SystemTask, string) {
	t.Helper()
	setupInvoiceEvidenceBatchTest(t)
	now := common.GetTimestamp()
	run := &InvoicePaymentEvidenceBackfillRun{
		PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion, CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256, PreviewExclusionReasons: "{}",
		Status: InvoicePaymentEvidenceRunStatusApplying, Attempt: 1, ActorID: 1, CutoverAuditJSON: "{}", CreatedAt: now,
	}
	require.NoError(t, DB.Create(run).Error)
	taskID, err := GenerateSystemTaskID()
	require.NoError(t, err)
	payload, err := common.Marshal(InvoicePaymentEvidenceApplyTaskPayload{
		Phase: "apply", RunID: run.ID, Attempt: run.Attempt, PolicySHA256: run.PolicySHA256,
	})
	require.NoError(t, err)
	activeKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, run.ID)
	runnerID := "invoice-runner"
	task := &SystemTask{TaskID: taskID, Type: constant.InvoicePaymentEvidenceTaskType, Status: SystemTaskStatusRunning,
		ActiveKey: &activeKey, Payload: string(payload), State: "null", Result: "null", LockedBy: runnerID}
	require.NoError(t, DB.Create(task).Error)
	require.NoError(t, DB.Create(&SystemTaskLock{Type: task.Type, TaskID: task.TaskID, LockedBy: runnerID, LockedUntil: now + 60}).Error)
	require.NoError(t, DB.Model(run).Update("active_task_id", task.TaskID).Error)
	run.ActiveTaskID = &task.TaskID
	return run, task, runnerID
}

func TestInvoiceEvidenceTerminalCompletionFencesTaskAndRun(t *testing.T) {
	run, task, runnerID := seedFencedInvoicePaymentEvidenceTask(t)
	require.NoError(t, CompleteInvoicePaymentEvidenceApply(context.Background(), run.ID, task.TaskID, runnerID, run.Attempt))

	var reloadedRun InvoicePaymentEvidenceBackfillRun
	require.NoError(t, DB.First(&reloadedRun, run.ID).Error)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusCompleted, reloadedRun.Status)
	assert.Nil(t, reloadedRun.ActiveTaskID)
	require.NotNil(t, reloadedRun.LastTaskID)
	assert.Equal(t, task.TaskID, *reloadedRun.LastTaskID)
	var reloadedTask SystemTask
	require.NoError(t, DB.First(&reloadedTask, task.ID).Error)
	assert.Equal(t, SystemTaskStatusSucceeded, reloadedTask.Status)
	assert.Nil(t, reloadedTask.ActiveKey)
	var locks int64
	require.NoError(t, DB.Model(&SystemTaskLock{}).Where("type = ?", task.Type).Count(&locks).Error)
	assert.Zero(t, locks)
}

func TestInvoiceEvidenceLiveFailureFencesTaskAndRun(t *testing.T) {
	run, task, runnerID := seedFencedInvoicePaymentEvidenceTask(t)
	require.NoError(t, FailInvoicePaymentEvidenceApply(context.Background(), run.ID, task.TaskID, runnerID, run.Attempt, "apply_failed", "apply failed"))

	var reloadedRun InvoicePaymentEvidenceBackfillRun
	require.NoError(t, DB.First(&reloadedRun, run.ID).Error)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusFailed, reloadedRun.Status)
	assert.Nil(t, reloadedRun.ActiveTaskID)
	require.NotNil(t, reloadedRun.LastErrorCode)
	assert.Equal(t, "apply_failed", *reloadedRun.LastErrorCode)
	var reloadedTask SystemTask
	require.NoError(t, DB.First(&reloadedTask, task.ID).Error)
	assert.Equal(t, SystemTaskStatusFailed, reloadedTask.Status)
	assert.Equal(t, "apply_failed", reloadedTask.Error)
}

func TestInvoiceEvidenceBatchRejectsStaleRunner(t *testing.T) {
	run, task, _ := seedFencedInvoicePaymentEvidenceTask(t)
	_, err := RunInvoicePaymentEvidenceApplyBatch(context.Background(), run.ID, task.TaskID, "stale-runner", run.Attempt)
	assert.ErrorIs(t, err, ErrSystemTaskLockLost)

	var reloaded InvoicePaymentEvidenceBackfillRun
	require.NoError(t, DB.First(&reloaded, run.ID).Error)
	assert.Equal(t, 0, reloaded.CursorTopUpID)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusApplying, reloaded.Status)
}

func TestInvoiceEvidenceFenceAcceptsEqualLockTimestamp(t *testing.T) {
	run, task, runnerID := seedFencedInvoicePaymentEvidenceTask(t)
	var lock SystemTaskLock
	require.NoError(t, DB.Where("type = ?", task.Type).First(&lock).Error)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := lockInvoicePaymentEvidenceExecutionFence(tx, run.ID, task.TaskID, runnerID, run.Attempt, lock.LockedUntil)
		return err
	}))
}
