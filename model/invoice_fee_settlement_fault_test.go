package model

import (
	"errors"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceFeeSettlementFixture struct {
	userID      int
	application InvoiceApplication
	charge      InvoiceFeeLedgerEntry
	refund      InvoiceFeeLedgerEntry
}

func seedInvoiceFeeSettlementPendingRefund(t *testing.T) invoiceFeeSettlementFixture {
	t.Helper()
	userID, profile := setupInvoiceApplicationTest(t, 100, 20)
	createInvoiceTopUp(t, 21, userID, 100)
	application, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("invoice-fee-fault", profile, 21), nil)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota).Error)
	_, err = RejectInvoiceApplication(900, application.ID, constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota-20).Error)

	var persisted InvoiceApplication
	require.NoError(t, DB.First(&persisted, application.ID).Error)
	var charge, refund InvoiceFeeLedgerEntry
	require.NoError(t, DB.Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeCharge).First(&charge).Error)
	require.NoError(t, DB.Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeRefund).First(&refund).Error)
	return invoiceFeeSettlementFixture{userID: userID, application: persisted, charge: charge, refund: refund}
}

func assertInvoiceFeeSettlementFinancialSnapshot(
	t *testing.T,
	fixture invoiceFeeSettlementFixture,
	wantApplication InvoiceApplication,
	wantCharge InvoiceFeeLedgerEntry,
	wantRefund InvoiceFeeLedgerEntry,
	wantQuota int,
) {
	t.Helper()
	var application InvoiceApplication
	require.NoError(t, DB.First(&application, fixture.application.ID).Error)
	var charge, refund InvoiceFeeLedgerEntry
	require.NoError(t, DB.First(&charge, fixture.charge.ID).Error)
	require.NoError(t, DB.First(&refund, fixture.refund.ID).Error)
	var user User
	require.NoError(t, DB.First(&user, fixture.userID).Error)
	assert.Equal(t, wantApplication.FeeStatus, application.FeeStatus)
	assert.Equal(t, wantApplication.Status, application.Status)
	assert.Equal(t, wantApplication.FeeChargeEntryID, application.FeeChargeEntryID)
	assert.Equal(t, wantApplication.FeeRefundEntryID, application.FeeRefundEntryID)
	assert.Equal(t, wantCharge, charge)
	assert.Equal(t, wantRefund, refund)
	assert.Equal(t, wantQuota, user.Quota)
}

func TestInvoiceFeeSettlementPendingRefundCorruptionFailsClosedWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, fixture invoiceFeeSettlementFixture)
	}{
		{
			name: "issued_application",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceApplication{}).Where("id = ?", fixture.application.ID).
					Update("status", constant.InvoiceApplicationStatusIssued).Error)
			},
		},
		{
			name: "charge_pointer_targets_refund",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceApplication{}).Where("id = ?", fixture.application.ID).
					Update("fee_charge_entry_id", fixture.refund.ID).Error)
			},
		},
		{
			name: "refund_pointer_targets_charge",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceApplication{}).Where("id = ?", fixture.application.ID).
					Update("fee_refund_entry_id", fixture.charge.ID).Error)
			},
		},
		{
			name: "charge_idempotency_key_drift",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", fixture.charge.ID).
					Update("idempotency_key", "invoice_fee:charge:wrong").Error)
			},
		},
		{
			name: "charge_balance_out_of_range",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", fixture.charge.ID).
					Update("balance_before", common.MaxQuota+1).Error)
			},
		},
		{
			name: "charge_applied_at_not_positive",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", fixture.charge.ID).
					Update("applied_at", 0).Error)
			},
		},
		{
			name: "pending_refund_has_balance",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", fixture.refund.ID).
					Updates(map[string]any{"balance_before": 1, "balance_after": 21}).Error)
			},
		},
		{
			name: "pending_refund_has_applied_at",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", fixture.refund.ID).
					Update("applied_at", 1).Error)
			},
		},
		{
			name: "pending_refund_key_drift",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", fixture.refund.ID).
					Update("idempotency_key", "invoice_fee:refund:wrong").Error)
			},
		},
		{
			name: "refund_applied_before_credit",
			mutate: func(t *testing.T, fixture invoiceFeeSettlementFixture) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", fixture.refund.ID).
					Update("status", InvoiceFeeEntryStatusApplied).Error)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := seedInvoiceFeeSettlementPendingRefund(t)
			test.mutate(t, fixture)
			var application InvoiceApplication
			require.NoError(t, DB.First(&application, fixture.application.ID).Error)
			var charge, refund InvoiceFeeLedgerEntry
			require.NoError(t, DB.First(&charge, fixture.charge.ID).Error)
			require.NoError(t, DB.First(&refund, fixture.refund.ID).Error)
			var user User
			require.NoError(t, DB.First(&user, fixture.userID).Error)

			applied, err := ApplyPendingInvoiceFeeRefund(fixture.application.ID)
			assert.False(t, applied)
			assert.ErrorIs(t, err, ErrInvoiceStateConflict)
			assertInvoiceFeeSettlementFinancialSnapshot(t, fixture, application, charge, refund, user.Quota)
		})
	}
}

func TestInvoiceFeeSettlementAppliedRefundExtremeBalanceAndTimestampCorruptionFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, refundID int64)
	}{
		{
			name: "negative_balance",
			mutate: func(t *testing.T, refundID int64) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", refundID).Update("balance_before", -1).Error)
			},
		},
		{
			name: "balance_above_max_quota",
			mutate: func(t *testing.T, refundID int64) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", refundID).Update("balance_after", common.MaxQuota+1).Error)
			},
		},
		{
			name: "minimum_integer_balance",
			mutate: func(t *testing.T, refundID int64) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", refundID).Update("balance_before", math.MinInt).Error)
			},
		},
		{
			name: "maximum_integer_balance",
			mutate: func(t *testing.T, refundID int64) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", refundID).Update("balance_after", math.MaxInt).Error)
			},
		},
		{
			name: "zero_applied_at",
			mutate: func(t *testing.T, refundID int64) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", refundID).Update("applied_at", 0).Error)
			},
		},
		{
			name: "negative_applied_at",
			mutate: func(t *testing.T, refundID int64) {
				require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", refundID).Update("applied_at", -1).Error)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := seedInvoiceFeeSettlementPendingRefund(t)
			applied, err := ApplyPendingInvoiceFeeRefund(fixture.application.ID)
			require.NoError(t, err)
			require.True(t, applied)
			test.mutate(t, fixture.refund.ID)

			var application InvoiceApplication
			require.NoError(t, DB.First(&application, fixture.application.ID).Error)
			var charge, refund InvoiceFeeLedgerEntry
			require.NoError(t, DB.First(&charge, fixture.charge.ID).Error)
			require.NoError(t, DB.First(&refund, fixture.refund.ID).Error)
			var user User
			require.NoError(t, DB.First(&user, fixture.userID).Error)

			applied, err = ApplyPendingInvoiceFeeRefund(fixture.application.ID)
			assert.False(t, applied)
			assert.ErrorIs(t, err, ErrInvoiceStateConflict)
			assertInvoiceFeeSettlementFinancialSnapshot(t, fixture, application, charge, refund, user.Quota)
		})
	}
}

func TestInvoiceFeeSettlementLedgerWriteFailureRollsBackQuotaAndPointers(t *testing.T) {
	fixture := seedInvoiceFeeSettlementPendingRefund(t)
	fault := errors.New("injected invoice fee ledger write failure")
	callbackName := "test:invoice-fee-settlement-ledger-fault"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "invoice_fee_ledger_entries" {
			tx.AddError(fault)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })

	var application InvoiceApplication
	require.NoError(t, DB.First(&application, fixture.application.ID).Error)
	var charge, refund InvoiceFeeLedgerEntry
	require.NoError(t, DB.First(&charge, fixture.charge.ID).Error)
	require.NoError(t, DB.First(&refund, fixture.refund.ID).Error)
	var user User
	require.NoError(t, DB.First(&user, fixture.userID).Error)

	applied, err := ApplyPendingInvoiceFeeRefund(fixture.application.ID)
	assert.False(t, applied)
	assert.ErrorIs(t, err, fault)
	assertInvoiceFeeSettlementFinancialSnapshot(t, fixture, application, charge, refund, user.Quota)
}
