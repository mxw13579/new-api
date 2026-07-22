package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTerminalInvoiceSettlementRetryValidatesFinancialIntegrity(t *testing.T) {
	t.Run("cancelled applied refund rejects charge drift", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 20)
		createInvoiceTopUp(t, 811, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("terminal-cancel-charge-drift", profile, 811), nil)
		require.NoError(t, err)
		application, err = CancelInvoiceApplication(userID, application.ID)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", *application.FeeChargeEntryID).
			Update("idempotency_key", "corrupted-charge-key").Error)
		assertTerminalInvoiceRetryFailsWithoutMutation(t, userID, application.ID, func() error {
			_, retryErr := CancelInvoiceApplication(userID, application.ID)
			return retryErr
		})
	})

	t.Run("cancelled applied refund rejects refund arithmetic drift", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 20)
		createInvoiceTopUp(t, 814, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("terminal-cancel-refund-drift", profile, 814), nil)
		require.NoError(t, err)
		application, err = CancelInvoiceApplication(userID, application.ID)
		require.NoError(t, err)
		var refund InvoiceFeeLedgerEntry
		require.NoError(t, DB.First(&refund, *application.FeeRefundEntryID).Error)
		corruptedBalance := *refund.BalanceAfter - 1
		require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", refund.ID).
			Update("balance_after", corruptedBalance).Error)
		assertTerminalInvoiceRetryFailsWithoutMutation(t, userID, application.ID, func() error {
			_, retryErr := CancelInvoiceApplication(userID, application.ID)
			return retryErr
		})
	})

	t.Run("rejected pending refund rejects refund drift", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 20)
		createInvoiceTopUp(t, 812, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("terminal-reject-refund-drift", profile, 812), nil)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota).Error)
		application, err = RejectInvoiceApplication(900, application.ID,
			constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
		require.NoError(t, err)
		require.Equal(t, constant.InvoiceFeeStatusRefundPending, application.FeeStatus)
		require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", *application.FeeRefundEntryID).
			Update("idempotency_key", "corrupted-refund-key").Error)
		assertTerminalInvoiceRetryFailsWithoutMutation(t, userID, application.ID, func() error {
			_, retryErr := RejectInvoiceApplication(900, application.ID,
				constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
			return retryErr
		})
	})

	t.Run("rejected pending refund rejects charge timestamp drift", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 20)
		createInvoiceTopUp(t, 815, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("terminal-reject-charge-drift", profile, 815), nil)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota).Error)
		application, err = RejectInvoiceApplication(900, application.ID,
			constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
		require.NoError(t, err)
		require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("id = ?", *application.FeeChargeEntryID).
			Update("applied_at", 0).Error)
		assertTerminalInvoiceRetryFailsWithoutMutation(t, userID, application.ID, func() error {
			_, retryErr := RejectInvoiceApplication(900, application.ID,
				constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
			return retryErr
		})
	})

	t.Run("cancelled zero fee rejects pointer drift", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 0)
		createInvoiceTopUp(t, 813, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("terminal-zero-fee-drift", profile, 813), nil)
		require.NoError(t, err)
		application, err = CancelInvoiceApplication(userID, application.ID)
		require.NoError(t, err)
		bogusPointer := int64(999999)
		require.NoError(t, DB.Model(&InvoiceApplication{}).Where("id = ?", application.ID).
			Update("fee_charge_entry_id", bogusPointer).Error)
		assertTerminalInvoiceRetryFailsWithoutMutation(t, userID, application.ID, func() error {
			_, retryErr := CancelInvoiceApplication(userID, application.ID)
			return retryErr
		})
	})

	t.Run("cancelled zero fee rejects orphan ledger", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 0)
		createInvoiceTopUp(t, 816, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("terminal-zero-fee-ledger", profile, 816), nil)
		require.NoError(t, err)
		application, err = CancelInvoiceApplication(userID, application.ID)
		require.NoError(t, err)
		orphan := InvoiceFeeLedgerEntry{
			ApplicationID: application.ID, UserID: userID, EntryType: InvoiceFeeEntryTypeRefund,
			Quota: 0, IdempotencyKey: "invoice_fee:refund:" + application.ApplicationNo,
			Status: InvoiceFeeEntryStatusPending,
		}
		require.NoError(t, DB.Create(&orphan).Error)
		assertTerminalInvoiceRetryFailsWithoutMutation(t, userID, application.ID, func() error {
			_, retryErr := CancelInvoiceApplication(userID, application.ID)
			return retryErr
		})
	})
}

func assertTerminalInvoiceRetryFailsWithoutMutation(t *testing.T, userID int, applicationID int64, retry func() error) {
	t.Helper()
	var applicationBefore InvoiceApplication
	require.NoError(t, DB.First(&applicationBefore, applicationID).Error)
	var ledgerBefore []InvoiceFeeLedgerEntry
	require.NoError(t, DB.Where("application_id = ?", applicationID).Order("id").Find(&ledgerBefore).Error)
	var userBefore User
	require.NoError(t, DB.First(&userBefore, userID).Error)

	err := retry()

	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
	var applicationAfter InvoiceApplication
	require.NoError(t, DB.First(&applicationAfter, applicationID).Error)
	var ledgerAfter []InvoiceFeeLedgerEntry
	require.NoError(t, DB.Where("application_id = ?", applicationID).Order("id").Find(&ledgerAfter).Error)
	var userAfter User
	require.NoError(t, DB.First(&userAfter, userID).Error)
	assert.Equal(t, applicationBefore, applicationAfter)
	assert.Equal(t, ledgerBefore, ledgerAfter)
	assert.Equal(t, userBefore.Quota, userAfter.Quota)
}

func TestInvoiceFeeCacheInvalidationObservesCommittedBalanceAndFailureDoesNotRollback(t *testing.T) {
	userID, application, _, refund := createPendingInvoiceFeeRefund(t, "cache-committed-apply")
	calls := 0
	cacheFailure := errors.New("injected cache invalidation failure")
	previousInvalidator := invoiceFeeCacheInvalidator
	invoiceFeeCacheInvalidator = func(invalidatedUserID int) error {
		calls++
		assert.Equal(t, userID, invalidatedUserID)
		var user User
		require.NoError(t, DB.First(&user, userID).Error)
		assert.Equal(t, common.MaxQuota, user.Quota, "cache invalidation must run after commit")
		var persistedRefund InvoiceFeeLedgerEntry
		require.NoError(t, DB.First(&persistedRefund, refund.ID).Error)
		assert.Equal(t, InvoiceFeeEntryStatusApplied, persistedRefund.Status)
		return cacheFailure
	}
	t.Cleanup(func() { invoiceFeeCacheInvalidator = previousInvalidator })

	applied, err := ApplyPendingInvoiceFeeRefund(application.ID)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, 1, calls)
	var persistedApplication InvoiceApplication
	require.NoError(t, DB.First(&persistedApplication, application.ID).Error)
	assert.Equal(t, constant.InvoiceFeeStatusRefunded, persistedApplication.FeeStatus)
}

func TestInvoiceFeeChargeCacheInvalidationObservesCommittedBalanceAndFailureDoesNotRollback(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 100, 20)
	createInvoiceTopUp(t, 817, userID, 100)
	calls := 0
	cacheFailure := errors.New("injected charge cache invalidation failure")
	previousInvalidator := invoiceFeeCacheInvalidator
	invoiceFeeCacheInvalidator = func(invalidatedUserID int) error {
		calls++
		assert.Equal(t, userID, invalidatedUserID)
		var user User
		require.NoError(t, DB.First(&user, userID).Error)
		assert.Equal(t, 80, user.Quota, "cache invalidation must observe the committed charge")
		var ledgerCount int64
		require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).
			Where("user_id = ? AND entry_type = ? AND status = ?", userID, InvoiceFeeEntryTypeCharge, InvoiceFeeEntryStatusApplied).
			Count(&ledgerCount).Error)
		assert.Equal(t, int64(1), ledgerCount)
		return cacheFailure
	}
	t.Cleanup(func() { invoiceFeeCacheInvalidator = previousInvalidator })

	application, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("cache-committed-charge", profile, 817), nil)

	require.NoError(t, err)
	require.NotNil(t, application)
	assert.Equal(t, 1, calls)
	var persisted InvoiceApplication
	require.NoError(t, DB.First(&persisted, application.ID).Error)
	assert.Equal(t, constant.InvoiceFeeStatusPaid, persisted.FeeStatus)
}

func TestInvoiceFeeCacheInvalidationOnlyFollowsActualCommittedBalanceChange(t *testing.T) {
	userID, application, _, _ := createPendingInvoiceFeeRefund(t, "cache-no-balance-change")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota).Error)
	calls := 0
	previousInvalidator := invoiceFeeCacheInvalidator
	invoiceFeeCacheInvalidator = func(int) error {
		calls++
		return nil
	}
	t.Cleanup(func() { invoiceFeeCacheInvalidator = previousInvalidator })

	applied, err := ApplyPendingInvoiceFeeRefund(application.ID)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Zero(t, calls)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota-20).Error)
	applied, err = ApplyPendingInvoiceFeeRefund(application.ID)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, 1, calls)

	applied, err = ApplyPendingInvoiceFeeRefund(application.ID)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, 1, calls)
}
