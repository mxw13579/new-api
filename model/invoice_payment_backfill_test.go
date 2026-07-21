package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

func TestInvoiceEvidencePreviewWinnerRecoveryClassification(t *testing.T) {
	const policyIndex = "idx_invoice_payment_evidence_backfill_runs_policy_version"
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "postgres policy conflict", err: &pgconn.PgError{Code: "23505", ConstraintName: policyIndex}, want: true},
		{name: "postgres unrelated unique conflict", err: &pgconn.PgError{Code: "23505", ConstraintName: "uk_other"}, want: false},
		{name: "mysql policy conflict", err: &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry 'legacy_backfill_v1' for key 'invoice_payment_evidence_backfill_runs." + policyIndex + "'"}, want: true},
		{name: "mysql unrelated unique conflict", err: &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'uk_other'"}, want: false},
		{name: "sqlite policy conflict", err: errors.New("constraint failed: UNIQUE constraint failed: invoice_payment_evidence_backfill_runs.policy_version (2067)"), want: true},
		{name: "sqlite unrelated unique conflict", err: errors.New("constraint failed: UNIQUE constraint failed: invoice_payment_evidence_backfill_items.run_id, invoice_payment_evidence_backfill_items.topup_id (2067)"), want: false},
		{name: "cancellation", err: context.Canceled, want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, isInvoicePaymentEvidencePolicyVersionConflict(testCase.err))
		})
	}
}

func TestInvoiceEvidencePreviewDoesNotMaskUnrelatedFailureWithConcurrentWinner(t *testing.T) {
	db := setupInvoiceEvidenceFileSQLite(t)
	require.NoError(t, db.AutoMigrate(&TopUp{}, &SubscriptionOrder{}))
	sentinel := errors.New("preview scan failed")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	var once sync.Once
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:preview-unrelated-error", func(tx *gorm.DB) {
		if tx.Statement.Table != "invoice_payment_evidence_backfill_runs" {
			return
		}
		once.Do(func() {
			_, insertErr := sqlDB.Exec(`INSERT INTO invoice_payment_evidence_backfill_runs
(policy_version, canonical_policy_json, policy_sha256, cutoff_max_top_up_id, preview_candidate_count,
preview_amount_minor, preview_exclusion_reasons, status, cursor_top_up_id, attempt, actor_id,
cutover_audit_json, created_at)
VALUES (?, ?, ?, 0, 0, 0, '{}', ?, 0, 0, 1, '{}', 1)`,
				constant.InvoicePaymentEvidencePolicyVersion, constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
				constant.InvoicePaymentEvidencePolicySHA256, InvoicePaymentEvidenceRunStatusPreviewed)
			require.NoError(t, insertErr)
			tx.AddError(sentinel)
		})
	}))

	run, created, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, InvoicePaymentEvidencePreviewAttestation{
		DeploymentSHA: "0123456789abcdef0123456789abcdef01234567", ActiveInstanceCount: 1, MatchingInstanceCount: 1,
	})
	require.ErrorIs(t, err, sentinel)
	assert.Nil(t, run)
	assert.False(t, created)
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

type invoiceEvidenceQueryShape struct {
	subscriptionQueries int
	maxTopUpRows        int64
	maxItemRows         int64
}

func recordInvoiceEvidenceQueryShape(t *testing.T, callbackName string) *invoiceEvidenceQueryShape {
	t.Helper()
	shape := &invoiceEvidenceQueryShape{}
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		switch tx.Statement.Table {
		case "subscription_orders":
			shape.subscriptionQueries++
		case "top_ups":
			shape.maxTopUpRows = max(shape.maxTopUpRows, tx.RowsAffected)
		case "invoice_payment_evidence_backfill_items":
			shape.maxItemRows = max(shape.maxItemRows, tx.RowsAffected)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(callbackName))
	})
	return shape
}

func TestInvoiceEvidencePreviewGapUsesBoundedPrefetchQueries(t *testing.T) {
	setupInvoiceEvidenceBatchTest(t)
	require.NoError(t, DB.Exec("DELETE FROM top_ups").Error)
	require.NoError(t, DB.Exec("DELETE FROM subscription_orders").Error)
	topUps := make([]TopUp, 0, invoicePaymentEvidencePreviewBatchSize+1)
	for i := 0; i < invoicePaymentEvidencePreviewBatchSize+1; i++ {
		topUps = append(topUps, legacyBatchTopUp(7000+i, fmt.Sprintf("bounded-preview-%d", i), 1))
	}
	require.NoError(t, DB.CreateInBatches(&topUps, 100).Error)
	shape := recordInvoiceEvidenceQueryShape(t, "test:invoice-evidence-preview-query-shape")

	run, created, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, InvoicePaymentEvidencePreviewAttestation{
		DeploymentSHA: "0123456789abcdef0123456789abcdef01234567", ActiveInstanceCount: 1, MatchingInstanceCount: 1,
	})
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, run)
	assert.Equal(t, int64(invoicePaymentEvidencePreviewBatchSize+1), run.PreviewCandidateCount)
	assert.LessOrEqual(t, shape.maxTopUpRows, int64(invoicePaymentEvidencePreviewBatchSize))
	assert.LessOrEqual(t, shape.maxItemRows, int64(invoicePaymentEvidencePreviewBatchSize))
	assert.LessOrEqual(t, shape.subscriptionQueries, 4)
}

func TestInvoiceEvidenceTerminalReconciliationUsesBoundedPrefetchQueries(t *testing.T) {
	setupInvoiceEvidenceBatchTest(t)
	require.NoError(t, DB.Exec("DELETE FROM top_ups").Error)
	require.NoError(t, DB.Exec("DELETE FROM subscription_orders").Error)
	topUps := make([]TopUp, 0, invoicePaymentEvidencePreviewBatchSize+1)
	for i := 0; i < invoicePaymentEvidencePreviewBatchSize+1; i++ {
		topUps = append(topUps, legacyBatchTopUp(8000+i, fmt.Sprintf("bounded-terminal-%d", i), 1))
	}
	run := createInvoiceEvidenceApplyingRun(t, topUps)
	for {
		var result InvoicePaymentEvidenceApplyBatchResult
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			var err error
			result, err = ApplyInvoicePaymentEvidenceBatchTx(tx, run.ID)
			return err
		}))
		if !result.HasMore {
			break
		}
	}
	shape := recordInvoiceEvidenceQueryShape(t, "test:invoice-evidence-terminal-query-shape")

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ReconcileInvoicePaymentEvidenceBackfillTx(tx, run.ID)
	}))
	assert.LessOrEqual(t, shape.maxTopUpRows, int64(invoicePaymentEvidencePreviewBatchSize))
	assert.LessOrEqual(t, shape.maxItemRows, int64(invoicePaymentEvidencePreviewBatchSize))
	assert.LessOrEqual(t, shape.subscriptionQueries, 2)
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
