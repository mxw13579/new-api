package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInvoiceApplicationTest(t *testing.T, quota int, feeQuota int64) (int, *InvoiceProfile) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(
		&InvoiceProfile{}, &InvoiceApplication{}, &InvoiceItem{}, &InvoiceFeeLedgerEntry{},
		&InvoicePaymentEvidenceBackfillRun{}, &InvoicePaymentEvidenceBackfillItem{},
	))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM invoice_fee_ledger_entries")
		DB.Exec("DELETE FROM invoice_items")
		DB.Exec("DELETE FROM invoice_applications")
		DB.Exec("DELETE FROM invoice_profiles")
	})

	previous := *operation_setting.GetInvoiceSetting()
	*operation_setting.GetInvoiceSetting() = operation_setting.InvoiceSetting{
		PersonalEnabled: true, CompanyEnabled: true, ApplicationWindowDays: 30,
		MinimumAmountMinor: 1, FeeQuota: feeQuota, PDFRetentionDays: 30,
	}
	t.Cleanup(func() { *operation_setting.GetInvoiceSetting() = previous })

	user := User{Username: "invoice-app-user", Password: "password", Quota: quota}
	require.NoError(t, DB.Create(&user).Error)
	profile, err := CreateInvoiceProfile(user.Id, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: "Alice", IsDefault: true,
	})
	require.NoError(t, err)
	return user.Id, profile
}

func createInvoiceTopUp(t *testing.T, id, userID int, amount int64) {
	t.Helper()
	topUp := trustedInvoiceTopUp(id, userID, "invoice-trade-"+string(rune('A'+id)))
	topUp.PaidAmountMinor = ptr(amount)
	topUp.CompleteTime = time.Now().Unix()
	require.NoError(t, DB.Create(&topUp).Error)
}

func createInvoiceApplicationRequest(requestID string, profile *InvoiceProfile, topUpIDs ...int) dto.CreateInvoiceApplicationRequest {
	return dto.CreateInvoiceApplicationRequest{
		RequestID: requestID, ProfileID: profile.ID, ProfileVersion: profile.Version, TopUpIDs: topUpIDs,
	}
}

func TestCreateInvoiceApplicationClaimsSortedWholeTopUpsAndChargesAtomically(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 100, 25)
	createInvoiceTopUp(t, 31, userID, 300)
	createInvoiceTopUp(t, 12, userID, 125)

	application, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-charge", profile, 31, 12, 31), NewTopUpInvoicePaymentSource())
	require.NoError(t, err)
	assert.Equal(t, int64(425), application.AmountMinor)
	assert.Equal(t, constant.InvoiceFeeStatusPaid, application.FeeStatus)
	require.NotNil(t, application.FeeChargeEntryID)
	assert.Nil(t, application.FeeRefundEntryID)

	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, 75, user.Quota)

	var items []InvoiceItem
	require.NoError(t, DB.Where("application_id = ?", application.ID).Order("topup_id").Find(&items).Error)
	require.Len(t, items, 2)
	assert.Equal(t, []int{12, 31}, []int{items[0].TopUpID, items[1].TopUpID})
	assert.Equal(t, int64(425), items[0].PaidAmountMinor+items[1].PaidAmountMinor)

	var ledger []InvoiceFeeLedgerEntry
	require.NoError(t, DB.Where("application_id = ?", application.ID).Find(&ledger).Error)
	require.Len(t, ledger, 1)
	assert.Equal(t, InvoiceFeeEntryTypeCharge, ledger[0].EntryType)
	assert.Equal(t, InvoiceFeeEntryStatusApplied, ledger[0].Status)
	assert.Equal(t, 25, ledger[0].Quota)
}

func TestCreateInvoiceApplicationIdempotencyAndFingerprintConflict(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 100, 10)
	createInvoiceTopUp(t, 41, userID, 100)
	createInvoiceTopUp(t, 42, userID, 200)

	first, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-idempotent", profile, 42, 41), nil)
	require.NoError(t, err)
	second, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-idempotent", profile, 41, 42, 41), nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	require.NoError(t, DeleteInvoiceProfile(userID, dto.DeleteInvoiceProfileRequest{ID: profile.ID, ExpectedVersion: profile.Version}))
	afterProfileDeletion, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-idempotent", profile, 42, 41), nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, afterProfileDeletion.ID, "a committed idempotent result survives later profile deletion")

	_, err = CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-idempotent", profile, 41), nil)
	assert.ErrorIs(t, err, ErrInvoiceIdempotencyConflict)

	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, 90, user.Quota, "idempotent retry must not charge twice")
	var count int64
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCreateInvoiceApplicationZeroFeeHasNoLedgerOrPointers(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 50, 0)
	createInvoiceTopUp(t, 51, userID, 100)

	application, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-zero", profile, 51), nil)
	require.NoError(t, err)
	assert.Equal(t, constant.InvoiceFeeStatusNotRequired, application.FeeStatus)
	assert.Nil(t, application.FeeChargeEntryID)
	assert.Nil(t, application.FeeRefundEntryID)
	var count int64
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("application_id = ?", application.ID).Count(&count).Error)
	assert.Zero(t, count)
}

func TestCreateInvoiceApplicationInsufficientQuotaRollsBackEverything(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 5, 10)
	createInvoiceTopUp(t, 61, userID, 100)

	_, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-insufficient", profile, 61), nil)
	assert.ErrorIs(t, err, ErrInvoiceQuotaInsufficient)

	var applicationCount, itemCount, ledgerCount int64
	require.NoError(t, DB.Model(&InvoiceApplication{}).Count(&applicationCount).Error)
	require.NoError(t, DB.Model(&InvoiceItem{}).Count(&itemCount).Error)
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Count(&ledgerCount).Error)
	assert.Zero(t, applicationCount)
	assert.Zero(t, itemCount)
	assert.Zero(t, ledgerCount)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, 61).Error)
	assert.Nil(t, topUp.InvoiceApplicationID)
	assert.Equal(t, int64(1), *topUp.PaymentVersion)
}

func TestCancelAndRejectReleaseWithSingleRefundOrPendingCAS(t *testing.T) {
	t.Run("cancel_applies_one_refund", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 20)
		createInvoiceTopUp(t, 71, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("request-cancel", profile, 71), nil)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", 71).Updates(map[string]any{
			"payment_state": "partially_refunded", "refunded_amount_minor": int64(1),
		}).Error)

		cancelled, err := CancelInvoiceApplication(userID, application.ID)
		require.NoError(t, err)
		assert.Equal(t, constant.InvoiceApplicationStatusCancelled, cancelled.Status)
		assert.Equal(t, constant.InvoiceFeeStatusRefunded, cancelled.FeeStatus)
		_, err = CancelInvoiceApplication(userID, application.ID)
		require.NoError(t, err)

		var user User
		require.NoError(t, DB.First(&user, userID).Error)
		assert.Equal(t, 100, user.Quota)
		var refunds int64
		require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).
			Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeRefund).Count(&refunds).Error)
		assert.Equal(t, int64(1), refunds)
		var topUp TopUp
		require.NoError(t, DB.First(&topUp, 71).Error)
		assert.Nil(t, topUp.InvoiceApplicationID)
		assert.Equal(t, int64(3), *topUp.PaymentVersion)
	})

	t.Run("reject_leaves_pending_until_full_headroom", func(t *testing.T) {
		userID, profile := setupInvoiceApplicationTest(t, 100, 20)
		createInvoiceTopUp(t, 72, userID, 100)
		application, err := CreateInvoiceApplication(userID,
			createInvoiceApplicationRequest("request-reject", profile, 72), nil)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota).Error)

		rejected, err := RejectInvoiceApplication(900, application.ID, constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
		require.NoError(t, err)
		assert.Equal(t, constant.InvoiceApplicationStatusRejected, rejected.Status)
		assert.Equal(t, constant.InvoiceFeeStatusRefundPending, rejected.FeeStatus)
		var released TopUp
		require.NoError(t, DB.First(&released, 72).Error)
		assert.Nil(t, released.InvoiceApplicationID)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota-20).Error)

		applied, err := ApplyPendingInvoiceFeeRefund(application.ID)
		require.NoError(t, err)
		assert.True(t, applied)
		applied, err = ApplyPendingInvoiceFeeRefund(application.ID)
		require.NoError(t, err)
		assert.False(t, applied)
		var user User
		require.NoError(t, DB.First(&user, userID).Error)
		assert.Equal(t, common.MaxQuota, user.Quota)
	})
}

func TestInvoiceApplicationReviewStateMatrix(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 100, 10)
	createInvoiceTopUp(t, 75, userID, 100)
	application, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-review", profile, 75), nil)
	require.NoError(t, err)

	reviewing, err := TransitionInvoiceApplicationReview(900, application.ID,
		constant.InvoiceApplicationStatusSubmitted, constant.InvoiceApplicationStatusReviewing)
	require.NoError(t, err)
	assert.Equal(t, constant.InvoiceApplicationStatusReviewing, reviewing.Status)
	approved, err := TransitionInvoiceApplicationReview(900, application.ID,
		constant.InvoiceApplicationStatusReviewing, constant.InvoiceApplicationStatusApproved)
	require.NoError(t, err)
	assert.Equal(t, constant.InvoiceApplicationStatusApproved, approved.Status)

	_, err = TransitionInvoiceApplicationReview(900, application.ID,
		constant.InvoiceApplicationStatusApproved, constant.InvoiceApplicationStatusReviewing)
	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
	_, err = CancelInvoiceApplication(userID, application.ID)
	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
}

func TestIssuedInvoiceNeverReleasesTopUpsOrRefundsFee(t *testing.T) {
	userID, profile := setupInvoiceApplicationTest(t, 100, 15)
	createInvoiceTopUp(t, 81, userID, 100)
	application, err := CreateInvoiceApplication(userID,
		createInvoiceApplicationRequest("request-issued", profile, 81), nil)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&InvoiceApplication{}).Where("id = ?", application.ID).
		Update("status", constant.InvoiceApplicationStatusIssued).Error)

	_, err = CancelInvoiceApplication(userID, application.ID)
	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
	_, err = RejectInvoiceApplication(900, application.ID, constant.InvoiceApplicationStatusIssued, "late")
	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, 81).Error)
	require.NotNil(t, topUp.InvoiceApplicationID)
	assert.Equal(t, application.ID, *topUp.InvoiceApplicationID)
	assert.Equal(t, int64(2), *topUp.PaymentVersion)
}
