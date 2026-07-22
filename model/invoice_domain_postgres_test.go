package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPersonalInvoicePostgreSQLDomainContract(t *testing.T) {
	database, enabled := openInvoiceEvidencePostgreSQL(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN and TEST_INVOICE_POSTGRES_DATABASE to run personal invoice PostgreSQL domain contract")
	}
	installPersonalInvoicePostgreSQLTestDatabase(t, database)
	runPersonalInvoicePostgreSQLDomainContract(t)
}

func runPersonalInvoicePostgreSQLDomainContract(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &SubscriptionOrder{}))
	require.NoError(t, migratePersonalInvoiceStructures(DB))
	previousSetting := *operation_setting.GetInvoiceSetting()
	t.Cleanup(func() { *operation_setting.GetInvoiceSetting() = previousSetting })
	setFee := func(fee int64) {
		*operation_setting.GetInvoiceSetting() = operation_setting.InvoiceSetting{
			PersonalEnabled: true, CompanyEnabled: true, ApplicationWindowDays: 30,
			MinimumAmountMinor: 1, FeeQuota: fee, PDFRetentionDays: 30,
		}
	}

	t.Run("profile_expected_version_and_default_convergence", func(t *testing.T) {
		user := personalInvoicePostgreSQLUser(t, "profile", 1000)
		first, err := CreateInvoiceProfile(user.Id, dto.CreateInvoiceProfileRequest{
			Type: constant.InvoiceTypePersonal, Title: "First", IsDefault: true,
		})
		require.NoError(t, err)
		second, err := CreateInvoiceProfile(user.Id, dto.CreateInvoiceProfileRequest{
			Type: constant.InvoiceTypePersonal, Title: "Second",
		})
		require.NoError(t, err)
		third, err := CreateInvoiceProfile(user.Id, dto.CreateInvoiceProfileRequest{
			Type: constant.InvoiceTypePersonal, Title: "Third",
		})
		require.NoError(t, err)

		_, err = UpdateInvoiceProfile(user.Id, dto.UpdateInvoiceProfileRequest{
			ID: first.ID, ExpectedVersion: first.Version + 1, Title: first.Title, IsDefault: true,
		})
		assert.ErrorIs(t, err, ErrInvoiceProfileVersionConflict)

		var wait sync.WaitGroup
		errs := make(chan error, 2)
		for _, profile := range []*InvoiceProfile{second, third} {
			profile := profile
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, updateErr := UpdateInvoiceProfile(user.Id, dto.UpdateInvoiceProfileRequest{
					ID: profile.ID, ExpectedVersion: profile.Version, Title: profile.Title, IsDefault: true,
				})
				errs <- updateErr
			}()
		}
		wait.Wait()
		close(errs)
		for updateErr := range errs {
			require.NoError(t, updateErr)
		}
		var profiles []InvoiceProfile
		require.NoError(t, DB.Where("user_id = ? AND type = ?", user.Id, constant.InvoiceTypePersonal).Order("id").Find(&profiles).Error)
		defaults := 0
		for _, profile := range profiles {
			if profile.IsDefault {
				defaults++
			}
		}
		assert.Equal(t, 1, defaults)
		var displaced InvoiceProfile
		require.NoError(t, DB.First(&displaced, first.ID).Error)
		assert.False(t, displaced.IsDefault)
		assert.Equal(t, int64(2), displaced.Version)
	})

	t.Run("normalized_idempotency_exact_sum_and_positive_fee", func(t *testing.T) {
		setFee(25)
		user, profile := personalInvoicePostgreSQLUserAndProfile(t, "positive", 1000)
		personalInvoicePostgreSQLTopUp(t, 101, user.Id, "positive-a", 300)
		personalInvoicePostgreSQLTopUp(t, 102, user.Id, "positive-b", 125)
		request := createInvoiceApplicationRequest("pg-positive", profile, 102, 101, 102)
		application, err := CreateInvoiceApplication(user.Id, request, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(425), application.AmountMinor)
		assert.Equal(t, constant.InvoiceFeeStatusPaid, application.FeeStatus)
		require.NotNil(t, application.FeeChargeEntryID)
		assert.Nil(t, application.FeeRefundEntryID)

		var items []InvoiceItem
		require.NoError(t, DB.Where("application_id = ?", application.ID).Order("topup_id").Find(&items).Error)
		require.Len(t, items, 2)
		assert.Equal(t, []int{101, 102}, []int{items[0].TopUpID, items[1].TopUpID})
		assert.Equal(t, int64(425), items[0].PaidAmountMinor+items[1].PaidAmountMinor)
		var charges []InvoiceFeeLedgerEntry
		require.NoError(t, DB.Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeCharge).Find(&charges).Error)
		require.Len(t, charges, 1)
		assert.Equal(t, 25, charges[0].Quota)
		assert.Equal(t, InvoiceFeeEntryStatusApplied, charges[0].Status)

		retry, err := CreateInvoiceApplication(user.Id,
			createInvoiceApplicationRequest("pg-positive", profile, 101, 102, 101), nil)
		require.NoError(t, err)
		assert.Equal(t, application.ID, retry.ID)
		_, err = CreateInvoiceApplication(user.Id,
			createInvoiceApplicationRequest("pg-positive", profile, 101), nil)
		assert.ErrorIs(t, err, ErrInvoiceIdempotencyConflict)
		var persisted User
		require.NoError(t, DB.First(&persisted, user.Id).Error)
		assert.Equal(t, 975, persisted.Quota)
		require.NoError(t, DB.Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeCharge).Find(&charges).Error)
		assert.Len(t, charges, 1)
	})

	t.Run("insufficient_quota_rolls_back_everything", func(t *testing.T) {
		setFee(50)
		user, profile := personalInvoicePostgreSQLUserAndProfile(t, "insufficient", 10)
		personalInvoicePostgreSQLTopUp(t, 201, user.Id, "insufficient", 100)
		beforeApplications := personalInvoicePostgreSQLCount(t, &InvoiceApplication{})
		beforeItems := personalInvoicePostgreSQLCount(t, &InvoiceItem{})
		beforeLedger := personalInvoicePostgreSQLCount(t, &InvoiceFeeLedgerEntry{})

		_, err := CreateInvoiceApplication(user.Id,
			createInvoiceApplicationRequest("pg-insufficient", profile, 201), nil)
		assert.ErrorIs(t, err, ErrInvoiceQuotaInsufficient)
		assert.Equal(t, beforeApplications, personalInvoicePostgreSQLCount(t, &InvoiceApplication{}))
		assert.Equal(t, beforeItems, personalInvoicePostgreSQLCount(t, &InvoiceItem{}))
		assert.Equal(t, beforeLedger, personalInvoicePostgreSQLCount(t, &InvoiceFeeLedgerEntry{}))
		var topUp TopUp
		require.NoError(t, DB.First(&topUp, 201).Error)
		assert.Nil(t, topUp.InvoiceApplicationID)
		require.NotNil(t, topUp.PaymentVersion)
		assert.Equal(t, int64(1), *topUp.PaymentVersion)
		var persisted User
		require.NoError(t, DB.First(&persisted, user.Id).Error)
		assert.Equal(t, 10, persisted.Quota)
	})

	t.Run("zero_fee_has_no_ledger_or_pointers", func(t *testing.T) {
		setFee(0)
		user, profile := personalInvoicePostgreSQLUserAndProfile(t, "zero", 100)
		personalInvoicePostgreSQLTopUp(t, 301, user.Id, "zero", 100)
		application, err := CreateInvoiceApplication(user.Id,
			createInvoiceApplicationRequest("pg-zero", profile, 301), nil)
		require.NoError(t, err)
		assert.Equal(t, constant.InvoiceFeeStatusNotRequired, application.FeeStatus)
		assert.Nil(t, application.FeeChargeEntryID)
		assert.Nil(t, application.FeeRefundEntryID)
		assert.Zero(t, personalInvoicePostgreSQLCountWhere(t, &InvoiceFeeLedgerEntry{}, "application_id = ?", application.ID))
	})

	t.Run("release_refund_and_pending_cas_are_exactly_once", func(t *testing.T) {
		setFee(20)
		cancelUser, cancelProfile := personalInvoicePostgreSQLUserAndProfile(t, "cancel", 100)
		personalInvoicePostgreSQLTopUp(t, 401, cancelUser.Id, "cancel", 100)
		cancelApplication, err := CreateInvoiceApplication(cancelUser.Id,
			createInvoiceApplicationRequest("pg-cancel", cancelProfile, 401), nil)
		require.NoError(t, err)
		cancelled, err := CancelInvoiceApplication(cancelUser.Id, cancelApplication.ID)
		require.NoError(t, err)
		assert.Equal(t, constant.InvoiceFeeStatusRefunded, cancelled.FeeStatus)
		_, err = CancelInvoiceApplication(cancelUser.Id, cancelApplication.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(1), personalInvoicePostgreSQLCountWhere(t, &InvoiceFeeLedgerEntry{},
			"application_id = ? AND entry_type = ?", cancelApplication.ID, InvoiceFeeEntryTypeRefund))
		var cancelledTopUp TopUp
		require.NoError(t, DB.First(&cancelledTopUp, 401).Error)
		assert.Nil(t, cancelledTopUp.InvoiceApplicationID)
		assert.Equal(t, int64(3), *cancelledTopUp.PaymentVersion)
		var refundedUser User
		require.NoError(t, DB.First(&refundedUser, cancelUser.Id).Error)
		assert.Equal(t, 100, refundedUser.Quota)

		pendingUser, pendingProfile := personalInvoicePostgreSQLUserAndProfile(t, "pending", 100)
		personalInvoicePostgreSQLTopUp(t, 402, pendingUser.Id, "pending", 100)
		pendingApplication, err := CreateInvoiceApplication(pendingUser.Id,
			createInvoiceApplicationRequest("pg-pending", pendingProfile, 402), nil)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", pendingUser.Id).Update("quota", common.MaxQuota).Error)
		rejected, err := RejectInvoiceApplication(900, pendingApplication.ID,
			constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
		require.NoError(t, err)
		assert.Equal(t, constant.InvoiceFeeStatusRefundPending, rejected.FeeStatus)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", pendingUser.Id).Update("quota", common.MaxQuota-20).Error)
		applied, err := ApplyPendingInvoiceFeeRefund(pendingApplication.ID)
		require.NoError(t, err)
		assert.True(t, applied)
		applied, err = ApplyPendingInvoiceFeeRefund(pendingApplication.ID)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.Equal(t, int64(1), personalInvoicePostgreSQLCountWhere(t, &InvoiceFeeLedgerEntry{},
			"application_id = ? AND entry_type = ?", pendingApplication.ID, InvoiceFeeEntryTypeRefund))
		var pendingTopUp TopUp
		require.NoError(t, DB.First(&pendingTopUp, 402).Error)
		assert.Nil(t, pendingTopUp.InvoiceApplicationID)
		var fullUser User
		require.NoError(t, DB.First(&fullUser, pendingUser.Id).Error)
		assert.Equal(t, common.MaxQuota, fullUser.Quota)
	})

	t.Run("stale_payment_version_has_no_partial_mutation", func(t *testing.T) {
		user := personalInvoicePostgreSQLUser(t, "stale", 100)
		personalInvoicePostgreSQLTopUp(t, 501, user.Id, "stale-a", 100)
		personalInvoicePostgreSQLTopUp(t, 502, user.Id, "stale-b", 100)
		require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", 502).Update("payment_version", int64(2)).Error)
		err := DB.Transaction(func(tx *gorm.DB) error {
			_, claimErr := NewTopUpInvoicePaymentSource().ClaimTopUpsTx(tx, ClaimInvoiceTopUpsRequest{
				UserID: user.Id, ApplicationID: 99001,
				TopUps: []TopUpVersionExpectation{
					{TopUpID: 501, ExpectedPaymentVersion: 1},
					{TopUpID: 502, ExpectedPaymentVersion: 1},
				},
			})
			return claimErr
		})
		assert.ErrorIs(t, err, ErrInvoicePaymentSourceClaimConflict)
		var topUps []TopUp
		require.NoError(t, DB.Where("id IN ?", []int{501, 502}).Order("id").Find(&topUps).Error)
		require.Len(t, topUps, 2)
		assert.Nil(t, topUps[0].InvoiceApplicationID)
		assert.Nil(t, topUps[1].InvoiceApplicationID)
		assert.Equal(t, int64(1), *topUps[0].PaymentVersion)
		assert.Equal(t, int64(2), *topUps[1].PaymentVersion)
	})
}

func personalInvoicePostgreSQLUser(t *testing.T, suffix string, quota int) *User {
	t.Helper()
	user := &User{Username: "invoice-pg-" + suffix, Password: "password", Quota: quota}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func personalInvoicePostgreSQLUserAndProfile(t *testing.T, suffix string, quota int) (*User, *InvoiceProfile) {
	t.Helper()
	user := personalInvoicePostgreSQLUser(t, suffix, quota)
	profile, err := CreateInvoiceProfile(user.Id, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: "Invoice " + suffix, IsDefault: true,
	})
	require.NoError(t, err)
	return user, profile
}

func personalInvoicePostgreSQLTopUp(t *testing.T, id, userID int, tradeNo string, amountMinor int64) {
	t.Helper()
	topUp := trustedInvoiceTopUp(id, userID, "pg-"+tradeNo)
	topUp.PaidAmountMinor = ptr(amountMinor)
	topUp.CompleteTime = time.Now().Unix()
	require.NoError(t, DB.Create(&topUp).Error)
}

func personalInvoicePostgreSQLCount(t *testing.T, value any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(value).Count(&count).Error)
	return count
}

func personalInvoicePostgreSQLCountWhere(t *testing.T, value any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(value).Where(query, args...).Count(&count).Error)
	return count
}
