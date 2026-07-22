package service

import (
	"context"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormPostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var invoiceFeeLeaseDatabaseNamePattern = regexp.MustCompile(`^newapi_invoice_pe_[0-9a-f]{8}_[0-9a-f]{12}$`)

func openInvoiceFeeLeasePostgreSQL(t *testing.T) (*gorm.DB, bool) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	databaseName := os.Getenv("TEST_INVOICE_POSTGRES_DATABASE")
	if dsn == "" && databaseName == "" {
		return nil, false
	}
	require.NotEmpty(t, dsn, "TEST_POSTGRES_DSN is required")
	require.Regexp(t, invoiceFeeLeaseDatabaseNamePattern, databaseName, "TEST_INVOICE_POSTGRES_DATABASE")

	admin, err := gorm.Open(gormPostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("postgres admin connection failed")
	}
	var existing int64
	require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", databaseName).Scan(&existing).Error)
	require.Zero(t, existing)
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+databaseName+`" TEMPLATE template0`).Error)

	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = databaseName
	testSQL := stdlib.OpenDB(*config)
	db, err := gorm.Open(gormPostgres.New(gormPostgres.Config{Conn: testSQL, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + databaseName + `"`).Error
		t.Fatal("postgres isolated database connection failed")
	}
	var currentDatabase string
	if err := db.Raw("SELECT current_database()").Scan(&currentDatabase).Error; err != nil || currentDatabase != databaseName {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + databaseName + `"`).Error
		t.Fatal("postgres isolated database binding verification failed")
	}

	previousDB := model.DB
	previousMainType := common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousMainType)
		require.NoError(t, testSQL.Close())
		require.NoError(t, admin.Exec(`DROP DATABASE "`+databaseName+`"`).Error)
		require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", databaseName).Scan(&existing).Error)
		require.Zero(t, existing)
		adminSQL, closeErr := admin.DB()
		if closeErr == nil {
			require.NoError(t, adminSQL.Close())
		}
	})
	return db, true
}

func TestInvoiceFeeSettlementLeaseLossRetriesInFlightPendingRefundExactlyOnce(t *testing.T) {
	db, enabled := openInvoiceFeeLeasePostgreSQL(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN and TEST_INVOICE_POSTGRES_DATABASE to run the in-flight lease-loss contract")
	}
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.InvoiceApplication{}, &model.InvoiceFeeLedgerEntry{},
		&model.SystemTask{}, &model.SystemTaskLock{},
	))
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
	oldApplicationLocked := make(chan string)
	newApplicationAttempted := make(chan struct{})
	releaseOldTransaction := make(chan struct{})
	var applicationQueryCount atomic.Int32
	const oldQueryMarker = "invoice-fee-old-application-query"
	beforeCallback := "test:invoice-fee-before-application-lock"
	afterCallback := "test:invoice-fee-after-application-lock"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(beforeCallback, func(tx *gorm.DB) {
		if tx.Statement.Table != "invoice_applications" {
			return
		}
		switch applicationQueryCount.Add(1) {
		case 1:
			tx.InstanceSet(oldQueryMarker, true)
		case 2:
			close(newApplicationAttempted)
		}
	}))
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(afterCallback, func(tx *gorm.DB) {
		if _, marked := tx.InstanceGet(oldQueryMarker); !marked {
			return
		}
		oldApplicationLocked <- tx.Statement.SQL.String()
		<-releaseOldTransaction
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(beforeCallback)
		_ = db.Callback().Query().Remove(afterCallback)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oldOwnerDone := make(chan struct{})
	oldOwnerApplied := false
	var oldOwnerApplyErr error
	oldHandler := newInvoiceFeeRefundSettlementHandler(db, func(applicationID int64) (bool, error) {
		oldOwnerApplied, oldOwnerApplyErr = model.ApplyPendingInvoiceFeeRefund(applicationID)
		return oldOwnerApplied, oldOwnerApplyErr
	}, func() int64 { return 1000 })
	go func() {
		defer close(oldOwnerDone)
		oldHandler.Run(ctx, oldTask, "invoice-fee-old-owner")
	}()
	lockedSQL := <-oldApplicationLocked
	assert.Contains(t, strings.ToUpper(lockedSQL), "FOR UPDATE", "the old owner must hold the application row lock")

	require.NoError(t, db.Model(&model.SystemTaskLock{}).Where("task_id = ?", oldTask.TaskID).
		Update("locked_until", common.GetTimestamp()-1).Error)
	cancel()
	require.NoError(t, model.ExpireStaleSystemTaskLocks(common.GetTimestamp()))

	newTask := claimInvoiceFeeSettlementTask(t, db, "invoice-fee-new-owner")
	newOwnerDone := make(chan struct{})
	newOwnerApplied := false
	var newOwnerApplyErr error
	newHandler := newInvoiceFeeRefundSettlementHandler(db, func(applicationID int64) (bool, error) {
		newOwnerApplied, newOwnerApplyErr = model.ApplyPendingInvoiceFeeRefund(applicationID)
		return newOwnerApplied, newOwnerApplyErr
	}, func() int64 { return 1001 })
	go func() {
		defer close(newOwnerDone)
		newHandler.Run(context.Background(), newTask, "invoice-fee-new-owner")
	}()
	<-newApplicationAttempted
	close(releaseOldTransaction)
	<-oldOwnerDone
	<-newOwnerDone
	require.NoError(t, oldOwnerApplyErr)
	require.NoError(t, newOwnerApplyErr)
	assert.True(t, oldOwnerApplied)
	assert.False(t, newOwnerApplied, "the new owner must observe the in-flight owner's committed refund")

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
	assert.Equal(t, InvoiceFeeRefundSettlementResult{Scanned: 1, StillPending: 1}, newResult)

	applied, err := model.ApplyPendingInvoiceFeeRefund(application.ID)
	require.NoError(t, err)
	assert.False(t, applied)
	require.NoError(t, db.First(&persistedUser, user.Id).Error)
	assert.Equal(t, 100, persistedUser.Quota, "a later retry must remain a financial no-op")
}
