package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormPostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const invoiceRetentionDatabasePrefix = "newapi_invoice_retention_"

var invoiceRetentionDatabaseNamePattern = regexp.MustCompile(`^` + invoiceRetentionDatabasePrefix + `[a-z0-9_]{1,34}$`)

func TestInvoiceRetentionContextLossLeavesReplayableClaim(t *testing.T) {
	assertInvoiceRetentionContextLossRace(t, openInvoiceDocumentServiceTestDB(t), "sqlite-context-loss")
}

func TestInvoiceRetentionPostgreSQLRaces(t *testing.T) {
	db, enabled := openInvoiceRetentionPostgreSQL(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN and TEST_INVOICE_POSTGRES_DATABASE to run invoice retention PostgreSQL races")
	}
	require.NoError(t, db.AutoMigrate(&model.InvoiceDocument{}))

	t.Run("two candidate readers produce one delete winner", func(t *testing.T) {
		document := seedInvoiceRetentionDocument(t, db, "double-read", model.InvoiceDocumentStatusSuperseded, 1)
		store := &invoiceRetentionRaceStore{bucket: document.R2Bucket}
		barrier := installInvoiceRetentionCandidateReadBarrier(t, db, 2)

		results := make(chan invoiceRetentionCleanupResult, 2)
		for worker := range 2 {
			ctx := context.WithValue(context.Background(), invoiceRetentionWorkerContextKey{}, worker)
			go func() {
				result, err := CleanupInvoiceDocuments(ctx, db, store, 100, 50, 1)
				results <- invoiceRetentionCleanupResult{result: result, err: err}
			}()
		}
		barrier.waitForReads(t)
		barrier.release()

		first, second := <-results, <-results
		require.NoError(t, first.err)
		require.NoError(t, second.err)
		assert.Equal(t, 1, first.result.Processed+second.result.Processed)
		assert.Equal(t, int32(1), store.deleteCalls.Load())
		assertInvoiceRetentionState(t, db, document.ID, model.InvoiceDocumentStatusDeleted, 1, "")
	})

	t.Run("stale takeover fences the old finalizer", func(t *testing.T) {
		document := seedInvoiceRetentionDocument(t, db, "stale-takeover", model.InvoiceDocumentStatusSuperseded, 1)
		firstDelete := make(chan struct{})
		secondDelete := make(chan struct{})
		releaseFirst := make(chan struct{})
		releaseSecond := make(chan struct{})
		store := &invoiceRetentionRaceStore{bucket: document.R2Bucket}
		store.delete = func(_ context.Context, _ string, call int32) error {
			switch call {
			case 1:
				close(firstDelete)
				<-releaseFirst
				return nil
			case 2:
				close(secondDelete)
				<-releaseSecond
				return ErrInvoiceObjectNotFound
			default:
				return fmt.Errorf("unexpected delete call %d", call)
			}
		}
		t.Cleanup(func() {
			closeInvoiceRetentionSignal(releaseFirst)
			closeInvoiceRetentionSignal(releaseSecond)
		})

		oldResult := make(chan invoiceRetentionCleanupResult, 1)
		go func() {
			result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 1)
			oldResult <- invoiceRetentionCleanupResult{result: result, err: err}
		}()
		waitInvoiceRetentionSignal(t, firstDelete, "old worker did not enter Delete")
		oldToken := loadInvoiceRetentionDocument(t, db, document.ID).OperationToken

		newResult := make(chan invoiceRetentionCleanupResult, 1)
		go func() {
			result, err := CleanupInvoiceDocuments(context.Background(), db, store, 200, 150, 1)
			newResult <- invoiceRetentionCleanupResult{result: result, err: err}
		}()
		waitInvoiceRetentionSignal(t, secondDelete, "takeover worker did not enter Delete")
		takenOver := loadInvoiceRetentionDocument(t, db, document.ID)
		assert.Equal(t, model.InvoiceDocumentStatusDeleting, takenOver.Status)
		assert.NotEqual(t, oldToken, takenOver.OperationToken)
		assert.Equal(t, 2, takenOver.DeleteAttempts)

		closeInvoiceRetentionSignal(releaseFirst)
		old := <-oldResult
		assert.ErrorIs(t, old.err, model.ErrInvoiceDocumentConflict)
		stillOwned := loadInvoiceRetentionDocument(t, db, document.ID)
		assert.Equal(t, model.InvoiceDocumentStatusDeleting, stillOwned.Status)
		assert.Equal(t, takenOver.OperationToken, stillOwned.OperationToken)

		closeInvoiceRetentionSignal(releaseSecond)
		newer := <-newResult
		require.NoError(t, newer.err)
		assertInvoiceRetentionState(t, db, document.ID, model.InvoiceDocumentStatusDeleted, 2, takenOver.OperationToken)
		assert.Equal(t, int32(2), store.deleteCalls.Load())
	})

	t.Run("context loss after delete leaves replayable deleting", func(t *testing.T) {
		assertInvoiceRetentionContextLossRace(t, db, "context-loss")
	})
}

func assertInvoiceRetentionContextLossRace(t *testing.T, db *gorm.DB, suffix string) {
	t.Helper()
	document := seedInvoiceRetentionDocument(t, db, suffix, model.InvoiceDocumentStatusSuperseded, 1)
	deleteEntered := make(chan struct{})
	releaseDelete := make(chan struct{})
	store := &invoiceRetentionRaceStore{bucket: document.R2Bucket}
	store.delete = func(_ context.Context, _ string, call int32) error {
		if call == 1 {
			close(deleteEntered)
			<-releaseDelete
			return nil
		}
		return ErrInvoiceObjectNotFound
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan invoiceRetentionCleanupResult, 1)
	go func() {
		result, err := CleanupInvoiceDocuments(ctx, db, store, 100, 50, 1)
		resultCh <- invoiceRetentionCleanupResult{result: result, err: err}
	}()
	waitInvoiceRetentionSignal(t, deleteEntered, "worker did not enter blocking Delete")
	cancel()
	close(releaseDelete)
	lost := <-resultCh
	assert.ErrorIs(t, lost.err, context.Canceled)
	claimed := loadInvoiceRetentionDocument(t, db, document.ID)
	assert.Equal(t, model.InvoiceDocumentStatusDeleting, claimed.Status)
	assert.Equal(t, 1, claimed.DeleteAttempts)

	replayed, err := CleanupInvoiceDocuments(context.Background(), db, store, 200, 150, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, replayed.Deleted)
	assertInvoiceRetentionState(t, db, document.ID, model.InvoiceDocumentStatusDeleted, 2, "")
	assert.Equal(t, int32(2), store.deleteCalls.Load())
}

type invoiceRetentionCleanupResult struct {
	result InvoiceDocumentCleanupResult
	err    error
}

type invoiceRetentionWorkerContextKey struct{}

type invoiceRetentionCandidateReadBarrier struct {
	reads   chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func installInvoiceRetentionCandidateReadBarrier(t *testing.T, db *gorm.DB, workers int) *invoiceRetentionCandidateReadBarrier {
	t.Helper()
	barrier := &invoiceRetentionCandidateReadBarrier{reads: make(chan struct{}, workers), proceed: make(chan struct{})}
	callbackName := "test:invoice-retention-candidate-read"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "invoice_documents" || tx.Statement.Context.Value(invoiceRetentionWorkerContextKey{}) == nil {
			return
		}
		barrier.reads <- struct{}{}
		<-barrier.proceed
	}))
	t.Cleanup(func() {
		barrier.release()
		_ = db.Callback().Query().Remove(callbackName)
	})
	return barrier
}

func (barrier *invoiceRetentionCandidateReadBarrier) waitForReads(t *testing.T) {
	t.Helper()
	for range 2 {
		waitInvoiceRetentionSignal(t, barrier.reads, "workers did not both complete the candidate read")
	}
}

func (barrier *invoiceRetentionCandidateReadBarrier) release() {
	barrier.once.Do(func() { close(barrier.proceed) })
}

type invoiceRetentionRaceStore struct {
	bucket      string
	deleteCalls atomic.Int32
	delete      func(context.Context, string, int32) error
}

func (store *invoiceRetentionRaceStore) Bucket() string { return store.bucket }

func (*invoiceRetentionRaceStore) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func (*invoiceRetentionRaceStore) Copy(context.Context, string, string) error { return nil }

func (*invoiceRetentionRaceStore) Head(context.Context, string) (InvoiceObjectHead, error) {
	return InvoiceObjectHead{}, nil
}

func (store *invoiceRetentionRaceStore) Delete(ctx context.Context, key string) error {
	call := store.deleteCalls.Add(1)
	if store.delete == nil {
		return nil
	}
	return store.delete(ctx, key, call)
}

func (*invoiceRetentionRaceStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func openInvoiceRetentionPostgreSQL(t *testing.T) (*gorm.DB, bool) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	databaseName := os.Getenv("TEST_INVOICE_POSTGRES_DATABASE")
	if dsn == "" && databaseName == "" {
		return nil, false
	}
	require.NotEmpty(t, dsn, "TEST_POSTGRES_DSN is required")
	require.Regexp(t, invoiceRetentionDatabaseNamePattern, databaseName, "TEST_INVOICE_POSTGRES_DATABASE must be a unique invoice retention database")

	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = databaseName
	admin, err := gorm.Open(gormPostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("PostgreSQL admin connection failed")
	}
	var existing int64
	require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", databaseName).Scan(&existing).Error)
	require.Zero(t, existing, "isolated PostgreSQL database already exists")
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+databaseName+`" TEMPLATE template0`).Error)

	testSQL := stdlib.OpenDB(*config)
	db, err := gorm.Open(gormPostgres.New(gormPostgres.Config{Conn: testSQL, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + databaseName + `"`).Error
		t.Fatal("isolated PostgreSQL connection failed")
	}
	var currentDatabase string
	if err := db.Raw("SELECT current_database()").Scan(&currentDatabase).Error; err != nil || currentDatabase != databaseName {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + databaseName + `"`).Error
		t.Fatal("isolated PostgreSQL binding verification failed before writes")
	}

	previousMainType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	t.Cleanup(func() {
		common.SetMainDatabaseType(previousMainType)
		require.NoError(t, testSQL.Close())
		require.NoError(t, admin.Exec(`DROP DATABASE "`+databaseName+`"`).Error)
		var exactResidual, prefixResidual int64
		require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", databaseName).Scan(&exactResidual).Error)
		require.Zero(t, exactResidual, "invoice retention PostgreSQL cleanup left the exact database")
		require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE LEFT(datname, LENGTH(?)) = ?", invoiceRetentionDatabasePrefix, invoiceRetentionDatabasePrefix).Scan(&prefixResidual).Error)
		require.Zero(t, prefixResidual, "invoice retention PostgreSQL cleanup left a prefixed database")
		adminSQL, closeErr := admin.DB()
		if closeErr == nil {
			require.NoError(t, adminSQL.Close())
		}
	})
	return db, true
}

func seedInvoiceRetentionDocument(t *testing.T, db *gorm.DB, suffix, status string, operationStartedAt int64) model.InvoiceDocument {
	t.Helper()
	key := "invoices/retention-" + suffix + ".pdf"
	document := model.InvoiceDocument{
		ApplicationID: 1, R2Bucket: "invoice-retention-test", ObjectKey: &key,
		ContentType: model.InvoicePDFContentType, SizeBytes: 100, SHA256: strings.Repeat("a", 64),
		Status: status, OperationToken: "retention-" + suffix + "-token", OperationStartedAt: operationStartedAt,
		UploadedBy: 1, UploadedAt: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&document).Error)
	return document
}

func loadInvoiceRetentionDocument(t *testing.T, db *gorm.DB, id int64) model.InvoiceDocument {
	t.Helper()
	var document model.InvoiceDocument
	require.NoError(t, db.First(&document, id).Error)
	return document
}

func assertInvoiceRetentionState(t *testing.T, db *gorm.DB, id int64, status string, attempts int, token string) {
	t.Helper()
	document := loadInvoiceRetentionDocument(t, db, id)
	assert.Equal(t, status, document.Status)
	assert.Equal(t, attempts, document.DeleteAttempts)
	if token != "" {
		assert.Equal(t, token, document.OperationToken)
	}
	if status == model.InvoiceDocumentStatusDeleted {
		require.NotNil(t, document.DeletedAt)
	}
}

func waitInvoiceRetentionSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatal(message)
	}
}

func closeInvoiceRetentionSignal(signal chan struct{}) {
	select {
	case <-signal:
	default:
		close(signal)
	}
}
