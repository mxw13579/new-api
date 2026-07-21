package service

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func setupInvoiceEvidenceServiceFileSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	dsn := filepath.Join(t.TempDir(), "invoice-evidence.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	assert.Equal(t, 8, sqlDB.Stats().MaxOpenConnections)
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&model.TopUp{}, &model.SubscriptionOrder{}, &model.InvoicePaymentEvidenceBackfillRun{},
		&model.InvoicePaymentEvidenceBackfillItem{}, &model.SystemTask{}, &model.SystemTaskLock{},
	))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		_ = sqlDB.Close()
	})
	return db
}

func TestInvoiceEvidenceConcurrentPreviewReturnsOneImmutableWinner(t *testing.T) {
	db := setupInvoiceEvidenceServiceFileSQLite(t)
	insertLegacyPreviewCandidate(t, "concurrent-preview", 1)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var blocked atomic.Int32
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:preview-contention", func(tx *gorm.DB) {
		if tx.Statement.Table == "invoice_payment_evidence_backfill_runs" && blocked.Add(1) <= 2 {
			arrived <- struct{}{}
			<-release
		}
	}))
	type previewResult struct {
		response *dto.BackfillPreviewResponse
		created  bool
		err      error
	}
	results := make(chan previewResult, 2)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response, created, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
			results <- previewResult{response: response, created: created, err: err}
		}()
	}
	close(start)
	<-arrived
	<-arrived
	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sqlDB.Stats().InUse, 2)
	close(release)
	wait.Wait()
	close(results)
	createdCount := 0
	var winnerID int64
	for result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.response)
		if winnerID == 0 {
			winnerID = result.response.RunID
		}
		assert.Equal(t, winnerID, result.response.RunID)
		if result.created {
			createdCount++
		}
	}
	assert.Equal(t, 1, createdCount)
}

func TestInvoiceEvidenceCancellationAfterPrivateItemRollsBack(t *testing.T) {
	db := setupInvoiceEvidenceServiceFileSQLite(t)
	insertLegacyPreviewCandidate(t, "cancel-after-private-item", 1)
	ctx, cancel := context.WithCancel(context.Background())
	itemWritten := make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("test:cancel-preview-item", func(tx *gorm.DB) {
		if tx.Statement.Table == "invoice_payment_evidence_backfill_items" {
			once.Do(func() {
				close(itemWritten)
				cancel()
			})
		}
	}))

	_, _, err := PreviewInvoicePaymentEvidenceBackfill(ctx, 1, validInvoiceEvidenceAttestation())
	<-itemWritten
	require.Error(t, err)
	var runs, items int64
	require.NoError(t, db.Model(&model.InvoicePaymentEvidenceBackfillRun{}).Count(&runs).Error)
	require.NoError(t, db.Model(&model.InvoicePaymentEvidenceBackfillItem{}).Count(&items).Error)
	assert.Zero(t, runs)
	assert.Zero(t, items)
}

func TestInvoiceEvidencePreviewAndGapScan501CandidatesInBoundedBatches(t *testing.T) {
	db := setupInvoiceEvidenceServiceFileSQLite(t)
	topUps := make([]model.TopUp, 0, 501)
	for i := 0; i < 501; i++ {
		topUps = append(topUps, model.TopUp{
			UserId: 1, Amount: 10, Money: 1, TradeNo: fmt.Sprintf("preview-batch-%03d", i),
			PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
			CompleteTime: 10, Status: common.TopUpStatusSuccess,
		})
	}
	require.NoError(t, db.CreateInBatches(&topUps, 100).Error)
	var batchRows []int64
	var batchRowsMu sync.Mutex
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:preview-batch-rows", func(tx *gorm.DB) {
		limitClause, ok := tx.Statement.Clauses["LIMIT"]
		limit, limitOK := limitClause.Expression.(clause.Limit)
		if tx.Statement.Table == "top_ups" && ok && limitOK && limit.Limit != nil && *limit.Limit == 500 {
			batchRowsMu.Lock()
			batchRows = append(batchRows, tx.RowsAffected)
			batchRowsMu.Unlock()
		}
	}))

	response, created, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, int64(501), response.CandidateCount)
	assert.Equal(t, int64(50100), response.AmountMinor)
	assert.Equal(t, topUps[len(topUps)-1].Id, response.CutoffMaxTopUpID)
	assert.Equal(t, []int64{500, 1, 0, 500, 1, 0}, batchRows)
	var run model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, db.First(&run, response.RunID).Error)
	assert.Zero(t, run.CursorTopUpID)
	assert.Equal(t, int64(501), run.PreviewCandidateCount)
	assert.Equal(t, int64(50100), run.PreviewAmountMinor)
	var itemCount int64
	require.NoError(t, db.Model(&model.InvoicePaymentEvidenceBackfillItem{}).Where("run_id = ?", run.ID).Count(&itemCount).Error)
	assert.Equal(t, int64(501), itemCount)
	winner, winnerCreated, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.NoError(t, err)
	assert.False(t, winnerCreated)
	assert.Equal(t, response.RunID, winner.RunID)
}

func TestInvoiceEvidenceConcurrentApplyReturnsOneBoundTask(t *testing.T) {
	db := setupInvoiceEvidenceServiceFileSQLite(t)
	insertLegacyPreviewCandidate(t, "concurrent-apply", 1)
	preview, _, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.NoError(t, err)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var blocked atomic.Int32
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:apply-contention", func(tx *gorm.DB) {
		if tx.Statement.Table == "system_tasks" && blocked.Add(1) <= 2 {
			arrived <- struct{}{}
			<-release
		}
	}))
	type applyResult struct {
		response *dto.BackfillApplyResponse
		err      error
	}
	results := make(chan applyResult, 2)
	start := make(chan struct{})
	request := dto.BackfillApplyRequest{ExpectedPolicySHA256: constant.InvoicePaymentEvidencePolicySHA256}
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response, err := ApplyInvoicePaymentEvidenceBackfill(context.Background(), 1, preview.RunID, request)
			results <- applyResult{response: response, err: err}
		}()
	}
	close(start)
	<-arrived
	<-arrived
	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sqlDB.Stats().InUse, 2)
	close(release)
	wait.Wait()
	close(results)
	createdCount := 0
	var taskID string
	for result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.response)
		if taskID == "" {
			taskID = result.response.TaskID
		}
		assert.Equal(t, taskID, result.response.TaskID)
		if result.response.Created {
			createdCount++
		}
	}
	assert.Equal(t, 1, createdCount)
}
