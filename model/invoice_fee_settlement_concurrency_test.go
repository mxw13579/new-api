package model

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInvoiceFeeSettlementConcurrentSQLite(t *testing.T, quota int, feePercent int64) (int, *InvoiceProfile) {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	dsn := filepath.Join(t.TempDir(), "invoice-fee-concurrency.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	require.NoError(t, DB.AutoMigrate(
		&User{}, &TopUp{}, &SubscriptionOrder{}, &InvoiceProfile{}, &InvoiceApplication{}, &InvoiceItem{},
		&InvoiceFeeLedgerEntry{}, &InvoicePaymentEvidenceBackfillRun{}, &InvoicePaymentEvidenceBackfillItem{},
	))
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		initCol()
		_ = sqlDB.Close()
	})

	previousSetting := operation_setting.GetInvoiceSetting()
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	operation_setting.PublishInvoiceSetting(operation_setting.InvoiceSetting{
		PersonalEnabled: true, CompanyEnabled: true, ApplicationWindowDays: 30,
		MinimumAmountMinor: 1, FeePercent: int(feePercent), PDFRetentionDays: 30,
	})
	t.Cleanup(func() {
		operation_setting.PublishInvoiceSetting(previousSetting)
		common.QuotaPerUnit = previousQuotaPerUnit
	})

	user := User{Username: "invoice-fee-concurrent", Password: "password", Quota: quota}
	require.NoError(t, DB.Create(&user).Error)
	profile, err := CreateInvoiceProfile(user.Id, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: "Concurrent Buyer", IsDefault: true,
	})
	require.NoError(t, err)
	return user.Id, profile
}

func createInvoiceFeeSettlementTopUp(t *testing.T, id, userID int) {
	t.Helper()
	topUp := trustedInvoiceTopUp(id, userID, "invoice-fee-concurrent-trade-"+string(rune('A'+id)))
	topUp.PaidAmountMinor = ptr(int64(100))
	topUp.CompleteTime = time.Now().Unix()
	require.NoError(t, DB.Create(&topUp).Error)
}

type invoiceFeeCreateResult struct {
	application *InvoiceApplication
	err         error
}

func runConcurrentInvoiceFeeCreates(
	userID int,
	profile *InvoiceProfile,
	requests []dto.CreateInvoiceApplicationRequest,
) []invoiceFeeCreateResult {
	start := make(chan struct{})
	results := make(chan invoiceFeeCreateResult, len(requests))
	var wait sync.WaitGroup
	for _, request := range requests {
		request := request
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			application, err := CreateInvoiceApplication(userID, request, nil)
			results <- invoiceFeeCreateResult{application: application, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	collected := make([]invoiceFeeCreateResult, 0, len(requests))
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

func TestInvoiceFeeSettlementConcurrentSameFingerprintChargesExactlyOnceSQLite(t *testing.T) {
	userID, profile := setupInvoiceFeeSettlementConcurrentSQLite(t, 100, 20)
	createInvoiceFeeSettlementTopUp(t, 1, userID)
	request := createInvoiceApplicationRequest("concurrent-same", profile, 1)

	results := runConcurrentInvoiceFeeCreates(userID, profile, []dto.CreateInvoiceApplicationRequest{request, request})
	require.Len(t, results, 2)
	for _, result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.application)
	}
	assert.Equal(t, results[0].application.ID, results[1].application.ID)

	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, 80, user.Quota)
	var applications, charges int64
	require.NoError(t, DB.Model(&InvoiceApplication{}).Count(&applications).Error)
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("entry_type = ?", InvoiceFeeEntryTypeCharge).Count(&charges).Error)
	assert.Equal(t, int64(1), applications)
	assert.Equal(t, int64(1), charges)
}

func TestInvoiceFeeSettlementConcurrentDifferentFingerprintConflictsWithoutExtraClaimSQLite(t *testing.T) {
	userID, profile := setupInvoiceFeeSettlementConcurrentSQLite(t, 100, 20)
	createInvoiceFeeSettlementTopUp(t, 2, userID)
	createInvoiceFeeSettlementTopUp(t, 3, userID)
	requestA := createInvoiceApplicationRequest("concurrent-conflict", profile, 2)
	requestB := createInvoiceApplicationRequest("concurrent-conflict", profile, 3)

	results := runConcurrentInvoiceFeeCreates(userID, profile, []dto.CreateInvoiceApplicationRequest{requestA, requestB})
	require.Len(t, results, 2)
	succeeded, conflicted := 0, 0
	for _, result := range results {
		switch {
		case result.err == nil:
			succeeded++
		case errors.Is(result.err, ErrInvoiceIdempotencyConflict):
			conflicted++
		default:
			require.NoError(t, result.err)
		}
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, conflicted)

	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, 80, user.Quota)
	var applications, charges, claims int64
	require.NoError(t, DB.Model(&InvoiceApplication{}).Count(&applications).Error)
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).Where("entry_type = ?", InvoiceFeeEntryTypeCharge).Count(&charges).Error)
	require.NoError(t, DB.Model(&TopUp{}).Where("invoice_application_id IS NOT NULL").Count(&claims).Error)
	assert.Equal(t, int64(1), applications)
	assert.Equal(t, int64(1), charges)
	assert.Equal(t, int64(1), claims)
}

func TestInvoiceFeeSettlementConcurrentApplyCreditsExactlyOnceSQLite(t *testing.T) {
	userID, profile := setupInvoiceFeeSettlementConcurrentSQLite(t, 100, 20)
	createInvoiceFeeSettlementTopUp(t, 4, userID)
	application, err := CreateInvoiceApplication(userID, createInvoiceApplicationRequest("concurrent-apply", profile, 4), nil)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota).Error)
	_, err = RejectInvoiceApplication(900, application.ID, constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Update("quota", common.MaxQuota-20).Error)

	start := make(chan struct{})
	results := make(chan invoiceFeeCreateResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			applied, applyErr := ApplyPendingInvoiceFeeRefund(application.ID)
			if applied {
				results <- invoiceFeeCreateResult{application: application}
				return
			}
			results <- invoiceFeeCreateResult{err: applyErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	appliedCount := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.application != nil {
			appliedCount++
		}
	}
	assert.Equal(t, 1, appliedCount)

	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, common.MaxQuota, user.Quota)
	var refunds int64
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).
		Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeRefund).Count(&refunds).Error)
	assert.Equal(t, int64(1), refunds)
}

func TestInvoiceFeeSettlementConcurrentCancelRejectCreatesSingleRefundSQLite(t *testing.T) {
	userID, profile := setupInvoiceFeeSettlementConcurrentSQLite(t, 100, 20)
	createInvoiceFeeSettlementTopUp(t, 5, userID)
	application, err := CreateInvoiceApplication(userID, createInvoiceApplicationRequest("concurrent-terminal", profile, 5), nil)
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, cancelErr := CancelInvoiceApplication(userID, application.ID)
		errs <- cancelErr
	}()
	go func() {
		defer wait.Done()
		<-start
		_, rejectErr := RejectInvoiceApplication(900, application.ID, constant.InvoiceApplicationStatusSubmitted, "invalid buyer facts")
		errs <- rejectErr
	}()
	close(start)
	wait.Wait()
	close(errs)
	succeeded, conflicted := 0, 0
	for terminalErr := range errs {
		if terminalErr == nil {
			succeeded++
		} else if errors.Is(terminalErr, ErrInvoiceStateConflict) {
			conflicted++
		} else {
			require.NoError(t, terminalErr)
		}
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, conflicted)

	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	assert.Equal(t, 100, user.Quota)
	var refunds int64
	require.NoError(t, DB.Model(&InvoiceFeeLedgerEntry{}).
		Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeRefund).Count(&refunds).Error)
	assert.Equal(t, int64(1), refunds)
}
