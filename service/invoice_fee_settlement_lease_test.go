package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceFeeSettlementLeaseLossRetriesSamePendingRefundExactlyOnce(t *testing.T) {
	db := openInvoiceFeeSettlementServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.InvoiceApplication{}))
	user := model.User{Id: 8101, Username: "invoice-fee-lease", Password: "password", Quota: 80}
	require.NoError(t, db.Create(&user).Error)
	appliedAt := int64(100)
	chargeBefore, chargeAfter := 100, 80
	application := model.InvoiceApplication{
		ID: 8201, ApplicationNo: "INV-lease-race", UserID: user.Id, RequestID: "lease-race",
		RequestFingerprint: "lease-race", Type: constant.InvoiceTypePersonal,
		Status: constant.InvoiceApplicationStatusCancelled, FeeQuota: 20,
		FeeMethod: model.InvoiceFeeMethodWalletQuota, FeeStatus: constant.InvoiceFeeStatusRefundPending,
		Currency: constant.InvoiceCurrencyCNY, ProfileSnapshot: "{}", PolicySnapshot: "{}", SubmittedAt: 1,
	}
	require.NoError(t, db.Create(&application).Error)
	charge := model.InvoiceFeeLedgerEntry{
		ApplicationID: application.ID, UserID: user.Id, EntryType: model.InvoiceFeeEntryTypeCharge,
		Quota: 20, IdempotencyKey: "invoice_fee:charge:" + application.ApplicationNo,
		BalanceBefore: &chargeBefore, BalanceAfter: &chargeAfter,
		Status: model.InvoiceFeeEntryStatusApplied, AppliedAt: &appliedAt,
	}
	refund := model.InvoiceFeeLedgerEntry{
		ApplicationID: application.ID, UserID: user.Id, EntryType: model.InvoiceFeeEntryTypeRefund,
		Quota: 20, IdempotencyKey: "invoice_fee:refund:" + application.ApplicationNo,
		Status: model.InvoiceFeeEntryStatusPending,
	}
	require.NoError(t, db.Create(&charge).Error)
	require.NoError(t, db.Create(&refund).Error)
	require.NoError(t, db.Model(&application).Updates(map[string]any{
		"fee_charge_entry_id": charge.ID, "fee_refund_entry_id": refund.ID,
	}).Error)

	oldTask := claimInvoiceFeeSettlementTask(t, db, "invoice-fee-old-owner")
	applyStarted := make(chan struct{})
	releaseOldOwner := make(chan struct{})
	oldOwnerApplied := false
	var oldOwnerApplyErr error
	oldHandler := newInvoiceFeeRefundSettlementHandler(db, func(applicationID int64) (bool, error) {
		close(applyStarted)
		<-releaseOldOwner
		oldOwnerApplied, oldOwnerApplyErr = model.ApplyPendingInvoiceFeeRefund(applicationID)
		return oldOwnerApplied, oldOwnerApplyErr
	}, func() int64 { return 1000 })
	ctx, cancel := context.WithCancel(context.Background())
	oldOwnerDone := make(chan struct{})
	go func() {
		defer close(oldOwnerDone)
		oldHandler.Run(ctx, oldTask, "invoice-fee-old-owner")
	}()
	<-applyStarted

	require.NoError(t, db.Model(&model.SystemTaskLock{}).Where("task_id = ?", oldTask.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)
	cancel()
	require.NoError(t, model.ExpireStaleSystemTaskLocks(common.GetTimestamp()))

	newTask := claimInvoiceFeeSettlementTask(t, db, "invoice-fee-new-owner")
	newInvoiceFeeRefundSettlementHandler(db, model.ApplyPendingInvoiceFeeRefund, func() int64 { return 1001 }).
		Run(context.Background(), newTask, "invoice-fee-new-owner")
	close(releaseOldOwner)
	<-oldOwnerDone
	require.NoError(t, oldOwnerApplyErr)
	assert.False(t, oldOwnerApplied, "the stale owner's in-flight retry must observe the new owner's applied result")

	var persistedUser model.User
	require.NoError(t, db.First(&persistedUser, user.Id).Error)
	assert.Equal(t, 100, persistedUser.Quota, "the same refund obligation must credit quota exactly once")
	var refundCount int64
	require.NoError(t, db.Model(&model.InvoiceFeeLedgerEntry{}).
		Where("application_id = ? AND entry_type = ?", application.ID, model.InvoiceFeeEntryTypeRefund).
		Count(&refundCount).Error)
	assert.Equal(t, int64(1), refundCount)
	require.NoError(t, db.First(&refund, refund.ID).Error)
	assert.Equal(t, model.InvoiceFeeEntryStatusApplied, refund.Status)

	var expiredOldTask, completedNewTask model.SystemTask
	require.NoError(t, db.First(&expiredOldTask, oldTask.ID).Error)
	require.NoError(t, db.First(&completedNewTask, newTask.ID).Error)
	assert.Equal(t, model.SystemTaskStatusFailed, expiredOldTask.Status)
	assert.Equal(t, "lease_expired", expiredOldTask.Error)
	assert.Equal(t, model.SystemTaskStatusSucceeded, completedNewTask.Status)
	var newResult InvoiceFeeRefundSettlementResult
	require.NoError(t, common.UnmarshalJsonStr(completedNewTask.Result, &newResult))
	assert.Equal(t, InvoiceFeeRefundSettlementResult{Scanned: 1, Applied: 1}, newResult)

	applied, err := model.ApplyPendingInvoiceFeeRefund(application.ID)
	require.NoError(t, err)
	assert.False(t, applied)
	require.NoError(t, db.First(&persistedUser, user.Id).Error)
	assert.Equal(t, 100, persistedUser.Quota, "a subsequent retry must remain a financial no-op")
}
