package model

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInvoiceEvidenceFileSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	dsn := filepath.Join(t.TempDir(), "invoice-evidence.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	assert.Equal(t, 8, sqlDB.Stats().MaxOpenConnections)
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&InvoicePaymentEvidenceBackfillRun{}, &InvoicePaymentEvidenceBackfillItem{}, &SystemTask{}, &SystemTaskLock{}))
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		_ = sqlDB.Close()
	})
	return db
}

func setupInvoiceEvidenceStopTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&InvoicePaymentEvidenceBackfillRun{}, &InvoicePaymentEvidenceBackfillItem{}))
	for _, table := range []string{"invoice_payment_evidence_backfill_items", "invoice_payment_evidence_backfill_runs", "system_task_locks", "system_tasks"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
}

func validCutoverAuditJSON(t *testing.T) string {
	t.Helper()
	payload, err := common.Marshal(InvoicePaymentEvidenceCutoverAudit{
		SchemaVersion: 1, DeploymentSHA: "0123456789abcdef0123456789abcdef01234567",
		ActiveInstanceCount: 1, MatchingInstanceCount: 1,
	})
	require.NoError(t, err)
	return string(payload)
}

func createBoundInvoiceEvidenceTask(t *testing.T, status SystemTaskStatus, attempt int64) (InvoicePaymentEvidenceBackfillRun, SystemTask, *SystemTaskLock) {
	t.Helper()
	run := InvoicePaymentEvidenceBackfillRun{
		PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion, CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256, Status: InvoicePaymentEvidenceRunStatusApplying,
		Attempt: attempt, ActorID: 1, CutoverAuditJSON: validCutoverAuditJSON(t), PreviewExclusionReasons: "{}",
	}
	require.NoError(t, DB.Create(&run).Error)
	taskID := fmt.Sprintf("systask_stop_%d", run.ID)
	activeKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, run.ID)
	payload, err := common.Marshal(InvoicePaymentEvidenceApplyTaskPayload{
		Phase: "apply", RunID: run.ID, Attempt: attempt, PolicySHA256: run.PolicySHA256,
	})
	require.NoError(t, err)
	task := SystemTask{TaskID: taskID, Type: constant.InvoicePaymentEvidenceTaskType, Status: status, ActiveKey: &activeKey, Payload: string(payload), State: "null", Result: "null"}
	var lock *SystemTaskLock
	if status == SystemTaskStatusRunning {
		task.LockedBy = "stop-runner"
		lock = &SystemTaskLock{Type: constant.InvoicePaymentEvidenceTaskType, TaskID: taskID, LockedBy: task.LockedBy, LockedUntil: common.GetTimestamp() + 60}
	}
	require.NoError(t, DB.Create(&task).Error)
	if lock != nil {
		require.NoError(t, DB.Create(lock).Error)
	}
	require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).Update("active_task_id", taskID).Error)
	run.ActiveTaskID = &taskID
	return run, task, lock
}

func TestInvoiceEvidenceStopPendingAndRunningAreAtomicallyFenced(t *testing.T) {
	for _, status := range []SystemTaskStatus{SystemTaskStatusPending, SystemTaskStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			setupInvoiceEvidenceStopTest(t)
			run, task, _ := createBoundInvoiceEvidenceTask(t, status, 1)

			result, err := StopInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
			require.NoError(t, err)
			assert.Equal(t, InvoicePaymentEvidenceRunStatusFailed, result.Status)
			assert.Equal(t, "operator_stopped", result.Reason)

			var stoppedTask SystemTask
			require.NoError(t, DB.Where("task_id = ?", task.TaskID).First(&stoppedTask).Error)
			assert.Equal(t, SystemTaskStatusFailed, stoppedTask.Status)
			assert.Nil(t, stoppedTask.ActiveKey)
			var lockCount int64
			require.NoError(t, DB.Model(&SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
			assert.Zero(t, lockCount)

			repeated, err := StopInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
			require.NoError(t, err)
			assert.Equal(t, result, repeated)
		})
	}
}

func TestInvoiceEvidenceStopRejectsCompletedAndMismatchedFence(t *testing.T) {
	setupInvoiceEvidenceStopTest(t)
	run, _, lock := createBoundInvoiceEvidenceTask(t, SystemTaskStatusRunning, 1)
	require.NotNil(t, lock)
	require.NoError(t, DB.Model(&SystemTaskLock{}).Where("type = ?", lock.Type).Update("locked_by", "other-runner").Error)
	_, err := StopInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
	require.ErrorIs(t, err, ErrInvoicePaymentEvidenceRunStateConflict)

	require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).
		Updates(map[string]any{"status": InvoicePaymentEvidenceRunStatusCompleted, "active_task_id": nil}).Error)
	_, err = StopInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
	require.ErrorIs(t, err, ErrInvoicePaymentEvidenceRunStateConflict)
}

func TestInvoiceEvidenceStopConcurrentCallsConverge(t *testing.T) {
	db := setupInvoiceEvidenceFileSQLite(t)
	run, _, _ := createBoundInvoiceEvidenceTask(t, SystemTaskStatusPending, 1)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var blocked atomic.Int32
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:stop-contention", func(tx *gorm.DB) {
		if tx.Statement.Table == "system_tasks" && blocked.Add(1) <= 2 {
			arrived <- struct{}{}
			<-release
		}
	}))
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := StopInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
			errorsSeen <- err
		}()
	}
	close(start)
	<-arrived
	<-arrived
	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sqlDB.Stats().InUse, 2)
	close(release)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}
}

func TestInvoiceEvidenceEnqueueRejectsEveryPersistedPolicyTamper(t *testing.T) {
	for _, field := range []string{"policy_version", "canonical_policy_json", "policy_sha256"} {
		t.Run(field, func(t *testing.T) {
			setupInvoiceEvidenceStopTest(t)
			run := InvoicePaymentEvidenceBackfillRun{
				PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion, CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
				PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256, Status: InvoicePaymentEvidenceRunStatusPreviewed,
				ActorID: 1, CutoverAuditJSON: validCutoverAuditJSON(t), PreviewExclusionReasons: "{}",
			}
			require.NoError(t, DB.Create(&run).Error)
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).Update(field, "tampered").Error)
			expected := constant.InvoicePaymentEvidencePolicySHA256
			if field == "policy_sha256" {
				expected = "tampered"
			}
			_, _, _, err := EnqueueInvoicePaymentEvidenceApply(context.Background(), run.ID, expected)
			require.ErrorIs(t, err, ErrInvoicePaymentEvidencePolicyMismatch)
		})
	}
}

func TestInvoiceEvidenceBackfillItemUsesFrozenTopupIDColumn(t *testing.T) {
	setupInvoiceEvidenceStopTest(t)
	columns, err := DB.Migrator().ColumnTypes(&InvoicePaymentEvidenceBackfillItem{})
	require.NoError(t, err)
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name())
	}
	assert.Contains(t, names, "topup_id")
	assert.NotContains(t, names, "top_up_id")
}

func TestInvoiceEvidenceDurableReconcilerConvergesFailedBoundTask(t *testing.T) {
	setupInvoiceEvidenceStopTest(t)
	run, task, _ := createBoundInvoiceEvidenceTask(t, SystemTaskStatusPending, 1)
	require.NoError(t, DB.Model(&SystemTask{}).Where("task_id = ?", task.TaskID).
		Updates(map[string]any{"status": SystemTaskStatusFailed, "active_key": nil, "error": "lease_expired"}).Error)

	require.NoError(t, ReconcileInvoicePaymentEvidenceBackfills(context.Background()))
	require.NoError(t, ReconcileInvoicePaymentEvidenceBackfills(context.Background()))

	var reconciled InvoicePaymentEvidenceBackfillRun
	require.NoError(t, DB.First(&reconciled, run.ID).Error)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusFailed, reconciled.Status)
	assert.Nil(t, reconciled.ActiveTaskID)
	require.NotNil(t, reconciled.LastTaskID)
	assert.Equal(t, task.TaskID, *reconciled.LastTaskID)
	require.NotNil(t, reconciled.LastErrorCode)
	assert.Equal(t, "lease_expired", *reconciled.LastErrorCode)
}
