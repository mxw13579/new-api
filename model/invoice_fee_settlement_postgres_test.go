package model

import (
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceFeeSettlementPostgreSQLCoreRaceContract(t *testing.T) {
	database, enabled := openInvoiceEvidencePostgreSQL(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN and TEST_INVOICE_POSTGRES_DATABASE to run invoice fee settlement PostgreSQL races")
	}
	installPersonalInvoicePostgreSQLTestDatabase(t, database)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &SubscriptionOrder{}))
	require.NoError(t, migratePersonalInvoiceStructures(DB))
	previousSetting := operation_setting.GetInvoiceSetting()
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	operation_setting.PublishInvoiceSetting(operation_setting.InvoiceSetting{
		PersonalEnabled: true, CompanyEnabled: true, ApplicationWindowDays: 30,
		MinimumAmountMinor: 1, FeePercent: 20, PDFRetentionDays: 30,
	})
	t.Cleanup(func() {
		operation_setting.PublishInvoiceSetting(previousSetting)
		common.QuotaPerUnit = previousQuotaPerUnit
	})

	t.Run("same_fingerprint_single_charge", func(t *testing.T) {
		user, profile := personalInvoicePostgreSQLUserAndProfile(t, "fee-race-same", 100)
		personalInvoicePostgreSQLTopUp(t, 7101, user.Id, "fee-race-same", 100)
		request := createInvoiceApplicationRequest("pg-fee-race-same", profile, 7101)
		results := runConcurrentInvoiceFeeCreates(user.Id, profile, []dto.CreateInvoiceApplicationRequest{request, request})
		require.Len(t, results, 2)
		for _, result := range results {
			require.NoError(t, result.err)
			require.NotNil(t, result.application)
		}
		assert.Equal(t, results[0].application.ID, results[1].application.ID)
		assert.Equal(t, int64(1), personalInvoicePostgreSQLCountWhere(t, &InvoiceFeeLedgerEntry{},
			"application_id = ? AND entry_type = ?", results[0].application.ID, InvoiceFeeEntryTypeCharge))
		var persisted User
		require.NoError(t, DB.First(&persisted, user.Id).Error)
		assert.Equal(t, 80, persisted.Quota)
	})

	t.Run("different_fingerprint_conflict_has_no_extra_claim", func(t *testing.T) {
		user, profile := personalInvoicePostgreSQLUserAndProfile(t, "fee-race-conflict", 100)
		personalInvoicePostgreSQLTopUp(t, 7201, user.Id, "fee-race-conflict-a", 100)
		personalInvoicePostgreSQLTopUp(t, 7202, user.Id, "fee-race-conflict-b", 100)
		requestA := createInvoiceApplicationRequest("pg-fee-race-conflict", profile, 7201)
		requestB := createInvoiceApplicationRequest("pg-fee-race-conflict", profile, 7202)
		results := runConcurrentInvoiceFeeCreates(user.Id, profile, []dto.CreateInvoiceApplicationRequest{requestA, requestB})
		require.Len(t, results, 2)
		succeeded, conflicted := 0, 0
		for _, result := range results {
			if result.err == nil {
				succeeded++
			} else if errors.Is(result.err, ErrInvoiceIdempotencyConflict) {
				conflicted++
			} else {
				require.NoError(t, result.err)
			}
		}
		assert.Equal(t, 1, succeeded)
		assert.Equal(t, 1, conflicted)
		assert.Equal(t, int64(1), personalInvoicePostgreSQLCountWhere(t, &InvoiceFeeLedgerEntry{},
			"user_id = ? AND entry_type = ?", user.Id, InvoiceFeeEntryTypeCharge))
		assert.Equal(t, int64(1), personalInvoicePostgreSQLCountWhere(t, &TopUp{},
			"user_id = ? AND invoice_application_id IS NOT NULL", user.Id))
	})

	t.Run("double_apply_single_credit", func(t *testing.T) {
		user, profile := personalInvoicePostgreSQLUserAndProfile(t, "fee-race-apply", 100)
		personalInvoicePostgreSQLTopUp(t, 7301, user.Id, "fee-race-apply", 100)
		application, err := CreateInvoiceApplication(user.Id,
			createInvoiceApplicationRequest("pg-fee-race-apply", profile, 7301), nil)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", common.MaxQuota).Error)
		_, err = RejectInvoiceApplication(900, application.ID, constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", common.MaxQuota-20).Error)

		start := make(chan struct{})
		results := make(chan bool, 2)
		errs := make(chan error, 2)
		var wait sync.WaitGroup
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				applied, applyErr := ApplyPendingInvoiceFeeRefund(application.ID)
				results <- applied
				errs <- applyErr
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		close(errs)
		for applyErr := range errs {
			require.NoError(t, applyErr)
		}
		appliedCount := 0
		for applied := range results {
			if applied {
				appliedCount++
			}
		}
		assert.Equal(t, 1, appliedCount)
		var persisted User
		require.NoError(t, DB.First(&persisted, user.Id).Error)
		assert.Equal(t, common.MaxQuota, persisted.Quota)
		assert.Equal(t, int64(1), personalInvoicePostgreSQLCountWhere(t, &InvoiceFeeLedgerEntry{},
			"application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeRefund))
	})

	t.Run("ledger_write_failure_rolls_back_quota_and_financial_state", func(t *testing.T) {
		user, profile := personalInvoicePostgreSQLUserAndProfile(t, "fee-ledger-rollback", 100)
		personalInvoicePostgreSQLTopUp(t, 7401, user.Id, "fee-ledger-rollback", 100)
		application, err := CreateInvoiceApplication(user.Id,
			createInvoiceApplicationRequest("pg-fee-ledger-rollback", profile, 7401), nil)
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", common.MaxQuota).Error)
		application, err = RejectInvoiceApplication(900, application.ID,
			constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
		require.NoError(t, err)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", common.MaxQuota-20).Error)

		var applicationBefore InvoiceApplication
		require.NoError(t, DB.First(&applicationBefore, application.ID).Error)
		var refundBefore InvoiceFeeLedgerEntry
		require.NoError(t, DB.First(&refundBefore, *application.FeeRefundEntryID).Error)
		fault := errors.New("injected PostgreSQL invoice fee ledger write failure")
		callbackName := "test:postgres-invoice-fee-ledger-fault"
		require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "invoice_fee_ledger_entries" {
				tx.AddError(fault)
			}
		}))
		t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })

		applied, err := ApplyPendingInvoiceFeeRefund(application.ID)

		assert.False(t, applied)
		assert.ErrorIs(t, err, fault)
		var persistedUser User
		require.NoError(t, DB.First(&persistedUser, user.Id).Error)
		assert.Equal(t, common.MaxQuota-20, persistedUser.Quota)
		var applicationAfter InvoiceApplication
		require.NoError(t, DB.First(&applicationAfter, application.ID).Error)
		assert.Equal(t, applicationBefore, applicationAfter)
		var refundAfter InvoiceFeeLedgerEntry
		require.NoError(t, DB.First(&refundAfter, refundBefore.ID).Error)
		assert.Equal(t, refundBefore, refundAfter)
	})
}
