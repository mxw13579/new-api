package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createPendingInvoiceFeeRefund(t *testing.T, requestID string) (int, *InvoiceApplication, *InvoiceFeeLedgerEntry, *InvoiceFeeLedgerEntry) {
	t.Helper()
	userID, profile := setupInvoiceApplicationTest(t, 100, 20)
	createInvoiceTopUp(t, 91, userID, 100)
	application, err := CreateInvoiceApplication(userID, createInvoiceApplicationRequest(requestID, profile, 91), nil)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota).Error)
	application, err = RejectInvoiceApplication(900, application.ID, constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
	require.NoError(t, err)
	require.Equal(t, constant.InvoiceFeeStatusRefundPending, application.FeeStatus)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota-20).Error)

	var charge, refund InvoiceFeeLedgerEntry
	require.NoError(t, DB.First(&charge, *application.FeeChargeEntryID).Error)
	require.NoError(t, DB.First(&refund, *application.FeeRefundEntryID).Error)
	return userID, application, &charge, &refund
}

func assertInvoiceFeeStateUnchanged(t *testing.T, userID int, application InvoiceApplication, charge, refund InvoiceFeeLedgerEntry) {
	t.Helper()
	var persistedApplication InvoiceApplication
	var persistedCharge, persistedRefund InvoiceFeeLedgerEntry
	var user User
	require.NoError(t, DB.First(&persistedApplication, application.ID).Error)
	require.NoError(t, DB.First(&persistedCharge, charge.ID).Error)
	require.NoError(t, DB.First(&persistedRefund, refund.ID).Error)
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, application.FeeChargeEntryID, persistedApplication.FeeChargeEntryID)
	assert.Equal(t, application.FeeRefundEntryID, persistedApplication.FeeRefundEntryID)
	assert.Equal(t, application.FeeStatus, persistedApplication.FeeStatus)
	assert.Equal(t, charge, persistedCharge)
	assert.Equal(t, refund, persistedRefund)
	assert.Equal(t, common.MaxQuota-20, user.Quota)
}

func TestApplyPendingInvoiceFeeRefundRequiresCancelledOrRejectedApplication(t *testing.T) {
	userID, application, charge, refund := createPendingInvoiceFeeRefund(t, "request-issued-retry")
	require.NoError(t, DB.Model(&InvoiceApplication{}).Where("id = ?", application.ID).
		Update("status", constant.InvoiceApplicationStatusIssued).Error)
	application.Status = constant.InvoiceApplicationStatusIssued

	applied, err := ApplyPendingInvoiceFeeRefund(application.ID)

	assert.False(t, applied)
	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
	assertInvoiceFeeStateUnchanged(t, userID, *application, *charge, *refund)
}

func TestApplyPendingInvoiceFeeRefundRejectsChargeLedgerDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*InvoiceApplication, *InvoiceFeeLedgerEntry, *InvoiceFeeLedgerEntry)
	}{
		{name: "nil pointer", mutate: func(application *InvoiceApplication, _, _ *InvoiceFeeLedgerEntry) { application.FeeChargeEntryID = nil }},
		{name: "wrong pointer", mutate: func(application *InvoiceApplication, _, refund *InvoiceFeeLedgerEntry) {
			application.FeeChargeEntryID = &refund.ID
		}},
		{name: "application", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) { charge.ApplicationID++ }},
		{name: "user", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) { charge.UserID++ }},
		{name: "type", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			charge.EntryType = "wrong"
		}},
		{name: "quota", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) { charge.Quota++ }},
		{name: "status", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			charge.Status = InvoiceFeeEntryStatusPending
		}},
		{name: "key", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) { charge.IdempotencyKey = "wrong" }},
		{name: "nil balance", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) { charge.BalanceAfter = nil }},
		{name: "negative balance", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			value := -1
			charge.BalanceBefore = &value
		}},
		{name: "balance above max", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			value := common.MaxQuota + 1
			charge.BalanceBefore = &value
		}},
		{name: "minimum integer balance", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			value := math.MinInt
			charge.BalanceBefore = &value
		}},
		{name: "maximum integer balance", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			value := math.MaxInt
			charge.BalanceAfter = &value
		}},
		{name: "wrong arithmetic", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			value := *charge.BalanceAfter + 1
			charge.BalanceAfter = &value
		}},
		{name: "zero applied at", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			value := int64(0)
			charge.AppliedAt = &value
		}},
		{name: "negative applied at", mutate: func(_ *InvoiceApplication, charge, _ *InvoiceFeeLedgerEntry) {
			value := int64(-1)
			charge.AppliedAt = &value
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userID, application, charge, refund := createPendingInvoiceFeeRefund(t, "request-charge-drift")
			test.mutate(application, charge, refund)
			require.NoError(t, DB.Save(application).Error)
			require.NoError(t, DB.Save(charge).Error)

			applied, err := ApplyPendingInvoiceFeeRefund(application.ID)

			assert.False(t, applied)
			assert.ErrorIs(t, err, ErrInvoiceStateConflict)
			assertInvoiceFeeStateUnchanged(t, userID, *application, *charge, *refund)
		})
	}
}

func TestApplyPendingInvoiceFeeRefundRejectsPendingRefundLedgerDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*InvoiceApplication, *InvoiceFeeLedgerEntry)
	}{
		{name: "nil pointer", mutate: func(application *InvoiceApplication, _ *InvoiceFeeLedgerEntry) { application.FeeRefundEntryID = nil }},
		{name: "application", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) { refund.ApplicationID++ }},
		{name: "user", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) { refund.UserID++ }},
		{name: "type", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) {
			refund.EntryType = "wrong"
		}},
		{name: "quota", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) { refund.Quota++ }},
		{name: "status", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) {
			refund.Status = InvoiceFeeEntryStatusApplied
		}},
		{name: "key", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) { refund.IdempotencyKey = "wrong" }},
		{name: "balance before", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) { value := 1; refund.BalanceBefore = &value }},
		{name: "balance after", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) { value := 1; refund.BalanceAfter = &value }},
		{name: "applied at", mutate: func(_ *InvoiceApplication, refund *InvoiceFeeLedgerEntry) {
			value := int64(1)
			refund.AppliedAt = &value
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userID, application, charge, refund := createPendingInvoiceFeeRefund(t, "request-refund-drift")
			test.mutate(application, refund)
			require.NoError(t, DB.Save(application).Error)
			require.NoError(t, DB.Save(refund).Error)

			applied, err := ApplyPendingInvoiceFeeRefund(application.ID)

			assert.False(t, applied)
			assert.ErrorIs(t, err, ErrInvoiceStateConflict)
			assertInvoiceFeeStateUnchanged(t, userID, *application, *charge, *refund)
		})
	}
}

func TestApplyPendingInvoiceFeeRefundValidatesAppliedRefundBeforeIdempotentReturn(t *testing.T) {
	t.Run("valid applied refund", func(t *testing.T) {
		userID, application, _, _ := createPendingInvoiceFeeRefund(t, "request-valid-refunded")
		applied, err := ApplyPendingInvoiceFeeRefund(application.ID)
		require.NoError(t, err)
		require.True(t, applied)

		applied, err = ApplyPendingInvoiceFeeRefund(application.ID)
		require.NoError(t, err)
		assert.False(t, applied)
		var user User
		require.NoError(t, DB.First(&user, userID).Error)
		assert.Equal(t, common.MaxQuota, user.Quota)
	})

	tests := []struct {
		name   string
		mutate func(*InvoiceFeeLedgerEntry)
	}{
		{name: "negative balance", mutate: func(refund *InvoiceFeeLedgerEntry) { value := -1; refund.BalanceBefore = &value }},
		{name: "balance above max", mutate: func(refund *InvoiceFeeLedgerEntry) { value := common.MaxQuota + 1; refund.BalanceAfter = &value }},
		{name: "minimum integer balance", mutate: func(refund *InvoiceFeeLedgerEntry) { value := math.MinInt; refund.BalanceBefore = &value }},
		{name: "maximum integer balance", mutate: func(refund *InvoiceFeeLedgerEntry) { value := math.MaxInt; refund.BalanceAfter = &value }},
		{name: "wrong arithmetic", mutate: func(refund *InvoiceFeeLedgerEntry) { value := *refund.BalanceAfter - 1; refund.BalanceAfter = &value }},
		{name: "zero applied at", mutate: func(refund *InvoiceFeeLedgerEntry) { value := int64(0); refund.AppliedAt = &value }},
		{name: "negative applied at", mutate: func(refund *InvoiceFeeLedgerEntry) { value := int64(-1); refund.AppliedAt = &value }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userID, application, charge, refund := createPendingInvoiceFeeRefund(t, "request-applied-drift")
			applied, err := ApplyPendingInvoiceFeeRefund(application.ID)
			require.NoError(t, err)
			require.True(t, applied)
			require.NoError(t, DB.First(application, application.ID).Error)
			require.NoError(t, DB.First(refund, refund.ID).Error)
			require.NoError(t, DB.First(charge, charge.ID).Error)
			test.mutate(refund)
			require.NoError(t, DB.Save(refund).Error)
			var user User
			require.NoError(t, DB.First(&user, userID).Error)
			quotaBefore := user.Quota

			applied, err = ApplyPendingInvoiceFeeRefund(application.ID)

			assert.False(t, applied)
			assert.ErrorIs(t, err, ErrInvoiceStateConflict)
			require.NoError(t, DB.First(&user, userID).Error)
			assert.Equal(t, quotaBefore, user.Quota)
			var persistedRefund InvoiceFeeLedgerEntry
			require.NoError(t, DB.First(&persistedRefund, refund.ID).Error)
			assert.Equal(t, *refund, persistedRefund)
		})
	}
}

func TestCancelInvoiceApplicationRejectsChargeDriftWithoutCreatingRefundOrReleasingTopUp(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 100, 20)
	createInvoiceTopUp(t, 92, userID, 100)
	application, err := CreateInvoiceApplication(userID, createInvoiceApplicationRequest("request-cancel-charge-drift", profile, 92), nil)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", *application.FeeChargeEntryID).
		Update("idempotency_key", "wrong").Error)

	_, err = CancelInvoiceApplication(userID, application.ID)

	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, 92).Error)
	require.NotNil(t, topUp.InvoiceApplicationID)
	assert.Equal(t, application.ID, *topUp.InvoiceApplicationID)
	var refunds int64
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).
		Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeRefund).Count(&refunds).Error)
	assert.Zero(t, refunds)
}

func TestCancelInvoiceApplicationDoesNotRepairOrphanRefundPointer(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 100, 20)
	createInvoiceTopUp(t, 93, userID, 100)
	application, err := CreateInvoiceApplication(userID, createInvoiceApplicationRequest("request-orphan-refund", profile, 93), nil)
	require.NoError(t, err)
	orphan := InvoiceFeeLedgerEntry{
		ApplicationID: application.ID, UserID: userID, EntryType: InvoiceFeeEntryTypeRefund,
		Quota: application.FeeQuota, IdempotencyKey: "invoice_fee:refund:" + application.ApplicationNo,
		Status: InvoiceFeeEntryStatusPending,
	}
	require.NoError(t, DB.Create(&orphan).Error)

	_, err = CancelInvoiceApplication(userID, application.ID)

	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
	var persisted InvoiceApplication
	require.NoError(t, DB.First(&persisted, application.ID).Error)
	assert.Nil(t, persisted.FeeRefundEntryID)
	assert.Equal(t, constant.InvoiceFeeStatusPaid, persisted.FeeStatus)
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, 80, user.Quota)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, 93).Error)
	require.NotNil(t, topUp.InvoiceApplicationID)
	assert.Equal(t, application.ID, *topUp.InvoiceApplicationID)
}
