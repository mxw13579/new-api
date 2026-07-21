package model

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceEvidenceRetryClassification(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "sqlite locked", err: errors.New("database is locked (5) (SQLITE_BUSY)"), want: true},
		{name: "sqlite busy", err: errors.New("database is busy"), want: true},
		{name: "mysql lock wait", err: &mysqlDriver.MySQLError{Number: 1205}, want: true},
		{name: "mysql deadlock", err: &mysqlDriver.MySQLError{Number: 1213}, want: true},
		{name: "postgres serialization", err: &pgconn.PgError{Code: "40001"}, want: true},
		{name: "postgres deadlock", err: &pgconn.PgError{Code: "40P01"}, want: true},
		{name: "not retryable", err: errors.New("constraint failed"), want: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isInvoicePaymentEvidenceRetryable(tc.err))
		})
	}
}

func setupInvoiceEvidenceBatchTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&InvoicePaymentEvidenceBackfillRun{}, &InvoicePaymentEvidenceBackfillItem{}))
	require.NoError(t, DB.Exec("DELETE FROM invoice_payment_evidence_backfill_items").Error)
	require.NoError(t, DB.Exec("DELETE FROM invoice_payment_evidence_backfill_runs").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM invoice_payment_evidence_backfill_items")
		DB.Exec("DELETE FROM invoice_payment_evidence_backfill_runs")
	})
}

func createInvoiceEvidenceApplyingRun(t *testing.T, topUps []TopUp) InvoicePaymentEvidenceBackfillRun {
	t.Helper()
	run := InvoicePaymentEvidenceBackfillRun{
		PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion, CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256, Status: InvoicePaymentEvidenceRunStatusApplying,
		PreviewCandidateCount: int64(len(topUps)), CutoffMaxTopUpID: topUps[len(topUps)-1].Id,
		PreviewExclusionReasons: "{}", CutoverAuditJSON: "{}", ActorID: 1,
	}
	require.NoError(t, DB.Create(&run).Error)
	for i := range topUps {
		require.NoError(t, DB.Create(&topUps[i]).Error)
		fingerprint, amount, err := invoicePaymentEvidenceFingerprint(&topUps[i])
		require.NoError(t, err)
		run.PreviewAmountMinor += amount
		require.NoError(t, DB.Create(&InvoicePaymentEvidenceBackfillItem{
			RunID: run.ID, TopUpID: topUps[i].Id, ExpectedAmountMinor: amount,
			SourceFingerprint: fingerprint, CreatedAt: 1,
		}).Error)
	}
	require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).
		Update("preview_amount_minor", run.PreviewAmountMinor).Error)
	return run
}

func legacyBatchTopUp(id int, tradeNo string, money float64) TopUp {
	return TopUp{Id: id, UserId: 1, Amount: 10, Money: money, TradeNo: tradeNo, PaymentMethod: "alipay",
		PaymentProvider: PaymentProviderEpay, CompleteTime: 10, Status: common.TopUpStatusSuccess}
}

func TestInvoiceEvidenceApplyBatchWritesLegacyFactsAndCursorAtomically(t *testing.T) {
	setupInvoiceEvidenceBatchTest(t)
	run := createInvoiceEvidenceApplyingRun(t, []TopUp{
		legacyBatchTopUp(6101, "legacy-batch-1", 1),
		legacyBatchTopUp(6102, "legacy-batch-2", 2),
	})

	var result InvoicePaymentEvidenceApplyBatchResult
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = ApplyInvoicePaymentEvidenceBatchTx(tx, run.ID)
		return err
	}))
	assert.Equal(t, int64(2), result.ProcessedCount)
	assert.Equal(t, 6102, result.LastTopUpID)
	assert.False(t, result.HasMore)

	var applied []TopUp
	require.NoError(t, DB.Order("id asc").Find(&applied, []int{6101, 6102}).Error)
	require.Len(t, applied, 2)
	for _, topUp := range applied {
		require.NotNil(t, topUp.PaymentEvidenceSource)
		assert.Equal(t, constant.InvoicePaymentEvidenceSourceLegacyBackfill, *topUp.PaymentEvidenceSource)
		require.NotNil(t, topUp.PaymentEvidenceRunID)
		assert.Equal(t, run.ID, *topUp.PaymentEvidenceRunID)
		require.NotNil(t, topUp.PaymentVersion)
		assert.Equal(t, int64(1), *topUp.PaymentVersion)
		assert.Nil(t, topUp.PaymentProviderTradeNo)
		assert.Nil(t, topUp.PaymentProviderTradeKey)
	}
	var updatedRun InvoicePaymentEvidenceBackfillRun
	require.NoError(t, DB.First(&updatedRun, run.ID).Error)
	assert.Equal(t, 6102, updatedRun.CursorTopUpID)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ReconcileInvoicePaymentEvidenceBackfillTx(tx, run.ID)
	}))
}

func TestInvoiceEvidenceApplyBatchAcceptsOnlyExactSameRunIdempotence(t *testing.T) {
	setupInvoiceEvidenceBatchTest(t)
	run := createInvoiceEvidenceApplyingRun(t, []TopUp{legacyBatchTopUp(6201, "legacy-idempotent", 1)})
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := ApplyInvoicePaymentEvidenceBatchTx(tx, run.ID)
		return err
	}))
	require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).Update("cursor_top_up_id", 0).Error)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := ApplyInvoicePaymentEvidenceBatchTx(tx, run.ID)
		return err
	}))

	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", 6201).Update("paid_amount_minor", 999).Error)
	require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).Update("cursor_top_up_id", 0).Error)
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, applyErr := ApplyInvoicePaymentEvidenceBatchTx(tx, run.ID)
		return applyErr
	})
	require.Error(t, err)
}

func TestInvoiceEvidenceTransactionRetriesAtMostThreeFreshAttempts(t *testing.T) {
	attempts := 0
	err := runInvoicePaymentEvidenceTransaction(context.Background(), DB, func(_ *gorm.DB) error {
		attempts++
		return errors.New("database is locked (5) (SQLITE_BUSY)")
	})
	require.Error(t, err)
	assert.Equal(t, 3, attempts)

	attempts = 0
	err = runInvoicePaymentEvidenceTransaction(context.Background(), DB, func(_ *gorm.DB) error {
		attempts++
		return errors.New("not retryable")
	})
	require.Error(t, err)
	assert.Equal(t, 1, attempts)
}
