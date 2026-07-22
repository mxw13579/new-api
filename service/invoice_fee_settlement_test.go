package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openInvoiceFeeSettlementServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.InvoiceFeeLedgerEntry{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	return db
}

func seedInvoiceFeeSettlementCandidates(t *testing.T, db *gorm.DB, count int) []model.InvoiceFeeLedgerEntry {
	t.Helper()
	entries := make([]model.InvoiceFeeLedgerEntry, count)
	for index := range entries {
		entries[index] = model.InvoiceFeeLedgerEntry{
			ApplicationID: int64(index + 1), UserID: 1, EntryType: model.InvoiceFeeEntryTypeRefund,
			Quota: 1, IdempotencyKey: fmt.Sprintf("refund-%d", index+1), Status: model.InvoiceFeeEntryStatusPending,
		}
	}
	if count > 0 {
		require.NoError(t, db.Create(&entries).Error)
	}
	return entries
}

func claimInvoiceFeeSettlementTask(t *testing.T, db *gorm.DB, runner string) *model.SystemTask {
	t.Helper()
	task, err := model.CreateSystemTask(model.InvoiceFeeRefundSettlementTaskType, nil, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, runner, common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	return claimed
}

func countInvoiceFeeCandidatePageQueries(t *testing.T, db *gorm.DB) *int {
	t.Helper()
	count := 0
	name := fmt.Sprintf("count-candidate-pages-%s-%p", strings.ReplaceAll(t.Name(), "/", "-"), &count)
	require.NoError(t, db.Callback().Row().After("gorm:row").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "invoice_fee_ledger_entries" && strings.Contains(tx.Statement.SQL.String(), "last_attempt_at") {
			count++
		}
	}))
	t.Cleanup(func() { db.Callback().Row().Remove(name) })
	return &count
}

func TestInvoiceFeeRefundSettlementRunHasLinearBoundaries(t *testing.T) {
	for _, candidateCount := range []int{0, 1, 100, 101} {
		t.Run(fmt.Sprintf("candidates_%d", candidateCount), func(t *testing.T) {
			db := openInvoiceFeeSettlementServiceTestDB(t)
			entries := seedInvoiceFeeSettlementCandidates(t, db, candidateCount)
			pageQueries := countInvoiceFeeCandidatePageQueries(t, db)
			applyCalls := 0
			handler := newInvoiceFeeRefundSettlementHandler(db, func(int64) (bool, error) {
				applyCalls++
				return false, nil
			}, func() int64 { return 1000 })

			task := claimInvoiceFeeSettlementTask(t, db, "runner")
			handler.Run(context.Background(), task, "runner")

			expected := min(candidateCount, invoiceFeeRefundSettlementBatchSize)
			assert.Equal(t, 1, *pageQueries)
			assert.Equal(t, expected, applyCalls)
			var finished model.SystemTask
			require.NoError(t, db.First(&finished, task.ID).Error)
			assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
			var result InvoiceFeeRefundSettlementResult
			require.NoError(t, common.UnmarshalJsonStr(finished.Result, &result))
			assert.Equal(t, InvoiceFeeRefundSettlementResult{Scanned: expected, StillPending: expected}, result)
			for index, entry := range entries {
				var current model.InvoiceFeeLedgerEntry
				require.NoError(t, db.First(&current, entry.ID).Error)
				if index < expected {
					assert.Equal(t, 1, current.AttemptCount)
				} else {
					assert.Zero(t, current.AttemptCount)
				}
			}
		})
	}
}

func TestInvoiceFeeRefundSettlementRotatesPoisonRowsWithinTwoRuns(t *testing.T) {
	db := openInvoiceFeeSettlementServiceTestDB(t)
	entries := seedInvoiceFeeSettlementCandidates(t, db, 101)
	legitimateApplicationID := entries[100].ApplicationID
	processedLegitimate := 0
	apply := func(applicationID int64) (bool, error) {
		if applicationID != legitimateApplicationID {
			return false, errors.New("sensitive database detail")
		}
		processedLegitimate++
		return true, db.Model(&model.InvoiceFeeLedgerEntry{}).
			Where("application_id = ?", applicationID).Update("status", model.InvoiceFeeEntryStatusApplied).Error
	}

	firstQueries := countInvoiceFeeCandidatePageQueries(t, db)
	newInvoiceFeeRefundSettlementHandler(db, apply, func() int64 { return 1000 }).
		Run(context.Background(), claimInvoiceFeeSettlementTask(t, db, "runner-1"), "runner-1")
	assert.Equal(t, 1, *firstQueries)
	assert.Zero(t, processedLegitimate)
	for _, entry := range entries[:100] {
		var current model.InvoiceFeeLedgerEntry
		require.NoError(t, db.First(&current, entry.ID).Error)
		assert.Equal(t, int64(1000), current.LastAttemptAt)
		assert.Equal(t, 1, current.AttemptCount)
		assert.Equal(t, invoiceFeeRefundSettlementApplyError, current.LastError)
	}

	secondQueries := countInvoiceFeeCandidatePageQueries(t, db)
	newInvoiceFeeRefundSettlementHandler(db, apply, func() int64 { return 1001 }).
		Run(context.Background(), claimInvoiceFeeSettlementTask(t, db, "runner-2"), "runner-2")
	assert.Equal(t, 1, *secondQueries)
	assert.Equal(t, 1, processedLegitimate)
}

func TestInvoiceFeeRefundSettlementLeaseLossDoesNotTerminalizeOldOwner(t *testing.T) {
	db := openInvoiceFeeSettlementServiceTestDB(t)
	entries := seedInvoiceFeeSettlementCandidates(t, db, 2)
	ctx, cancel := context.WithCancel(context.Background())
	credits := 0
	handler := newInvoiceFeeRefundSettlementHandler(db, func(applicationID int64) (bool, error) {
		credits++
		cancel()
		return true, db.Model(&model.InvoiceFeeLedgerEntry{}).
			Where("application_id = ?", applicationID).Update("status", model.InvoiceFeeEntryStatusApplied).Error
	}, func() int64 { return 1000 })
	task := claimInvoiceFeeSettlementTask(t, db, "runner-old")

	handler.Run(ctx, task, "runner-old")

	assert.Equal(t, 1, credits)
	var oldTask model.SystemTask
	require.NoError(t, db.First(&oldTask, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusRunning, oldTask.Status)
	var first, second model.InvoiceFeeLedgerEntry
	require.NoError(t, db.First(&first, entries[0].ID).Error)
	require.NoError(t, db.First(&second, entries[1].ID).Error)
	assert.Equal(t, 1, first.AttemptCount)
	assert.Zero(t, second.AttemptCount)

	require.NoError(t, db.Model(&model.SystemTaskLock{}).Where("task_id = ?", task.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)
	require.NoError(t, model.ExpireStaleSystemTaskLocks(common.GetTimestamp()))
	require.NoError(t, db.First(&oldTask, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusFailed, oldTask.Status)
	assert.Equal(t, "lease_expired", oldTask.Error)

	newInvoiceFeeRefundSettlementHandler(db, func(int64) (bool, error) {
		credits++
		return true, nil
	}, func() int64 { return 1001 }).Run(context.Background(), claimInvoiceFeeSettlementTask(t, db, "runner-new"), "runner-new")
	assert.Equal(t, 2, credits, "only the still-pending second obligation may be applied")
}
