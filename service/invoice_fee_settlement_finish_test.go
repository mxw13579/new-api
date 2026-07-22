package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func captureInvoiceFeeSettlementWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	common.LogWriterMu.Lock()
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = buffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previous
		common.LogWriterMu.Unlock()
	})
	return buffer
}

func TestInvoiceFeeRefundSettlementReportsFinishPersistenceFailureSafely(t *testing.T) {
	db := openInvoiceFeeSettlementServiceTestDB(t)
	warnings := captureInvoiceFeeSettlementWarnings(t)
	finishFailure := errors.New("sensitive storage failure")
	finishCalls := 0
	handler := newInvoiceFeeRefundSettlementHandler(db, model.ApplyPendingInvoiceFeeRefund, func() int64 { return 1000 })
	handler.finish = func(taskID, runnerID string, status model.SystemTaskStatus, result any, errorMessage string) error {
		finishCalls++
		assert.Equal(t, "fee-finish-failure", taskID)
		assert.Equal(t, "runner", runnerID)
		assert.Equal(t, model.SystemTaskStatusSucceeded, status)
		assert.Equal(t, InvoiceFeeRefundSettlementResult{}, result)
		assert.Empty(t, errorMessage)
		return finishFailure
	}

	handler.Run(context.Background(), &model.SystemTask{TaskID: "fee-finish-failure"}, "runner")

	assert.Equal(t, 1, finishCalls)
	assert.Contains(t, warnings.String(), "task_id=fee-finish-failure")
	assert.Contains(t, warnings.String(), "finish persistence failed")
	assert.NotContains(t, warnings.String(), finishFailure.Error())
}

func TestInvoiceFeeRefundSettlementFinishLockLossRemainsFenced(t *testing.T) {
	db := openInvoiceFeeSettlementServiceTestDB(t)
	warnings := captureInvoiceFeeSettlementWarnings(t)
	handler := newInvoiceFeeRefundSettlementHandler(db, model.ApplyPendingInvoiceFeeRefund, func() int64 { return 1000 })
	handler.finish = func(string, string, model.SystemTaskStatus, any, string) error {
		return model.ErrSystemTaskLockLost
	}

	handler.Run(context.Background(), &model.SystemTask{TaskID: "fee-lock-lost"}, "old-runner")

	assert.NotContains(t, warnings.String(), "finish persistence failed")
}

func TestInvoiceFeeRefundSettlementReportsEarlyFinishFailureSafely(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	warnings := captureInvoiceFeeSettlementWarnings(t)
	handler := newInvoiceFeeRefundSettlementHandler(db, model.ApplyPendingInvoiceFeeRefund, func() int64 { return 1000 })
	handler.finish = func(taskID, runnerID string, status model.SystemTaskStatus, result any, errorMessage string) error {
		assert.Equal(t, model.SystemTaskStatusFailed, status)
		assert.Equal(t, InvoiceFeeRefundSettlementResult{}, result)
		assert.Equal(t, invoiceFeeRefundSettlementTaskError, errorMessage)
		return errors.New("raw database detail")
	}

	handler.Run(context.Background(), &model.SystemTask{TaskID: "fee-query-failure"}, "runner")

	assert.Contains(t, warnings.String(), "task_id=fee-query-failure")
	assert.NotContains(t, warnings.String(), "raw database detail")
}
