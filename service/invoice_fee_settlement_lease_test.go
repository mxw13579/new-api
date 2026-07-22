package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceFeeSettlementCancelledOwnerDoesNotTerminalizeAndLeaseExpires(t *testing.T) {
	truncate(t)
	task, err := model.CreateSystemTask(model.InvoiceFeeRefundSettlementTaskType, nil, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "invoice-fee-old-owner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)

	var applyCalls atomic.Int32
	handler := newInvoiceFeeRefundSettlementHandler(model.DB, func(int64) (bool, error) {
		applyCalls.Add(1)
		return true, nil
	}, func() int64 { return common.GetTimestamp() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler.Run(ctx, claimed, claimed.LockedBy)

	var afterOwner model.SystemTask
	require.NoError(t, model.DB.First(&afterOwner, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusRunning, afterOwner.Status)
	assert.Zero(t, applyCalls.Load())
	require.NoError(t, model.DB.Model(&model.SystemTaskLock{}).Where("task_id = ?", task.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)
	require.NoError(t, model.ExpireStaleSystemTaskLocks(common.GetTimestamp()))

	var expired model.SystemTask
	require.NoError(t, model.DB.First(&expired, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusFailed, expired.Status)
	assert.Equal(t, "lease_expired", expired.Error)
}
