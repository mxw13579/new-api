package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedCleanupDocument(t *testing.T, db *gorm.DB, status, key string, expiresAt *int64, operationStartedAt int64) model.InvoiceDocument {
	t.Helper()
	document := model.InvoiceDocument{
		ApplicationID: 1, R2Bucket: "private", ObjectKey: &key, ContentType: model.InvoicePDFContentType,
		Status: status, OperationToken: key + "-token", OperationStartedAt: operationStartedAt,
		UploadedBy: 1, UploadedAt: 1, ExpiresAt: expiresAt, CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&document).Error)
	return document
}

func TestCleanupInvoiceDocumentsCoversStatusesLeaseAndNotFound(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	expired := int64(90)
	future := int64(200)
	candidates := []model.InvoiceDocument{
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusAvailable, "expired", &expired, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleteFailed, "retry", nil, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "old", nil, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleting, "stale", nil, 1),
	}
	ignoredAvailable := seedCleanupDocument(t, db, model.InvoiceDocumentStatusAvailable, "future", &future, 1)
	ignoredDeleting := seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleting, "fresh", nil, 99)
	store.objects["expired"] = []byte("pdf")
	store.objects["retry"] = []byte("pdf")
	store.objects["old"] = []byte("pdf")
	store.deleteErrors["stale"] = ErrInvoiceObjectNotFound

	result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)
	require.NoError(t, err)
	assert.Equal(t, 4, result.Processed)
	assert.Equal(t, 4, result.Deleted)
	for _, candidate := range candidates {
		current := model.InvoiceDocument{}
		require.NoError(t, db.First(&current, candidate.ID).Error)
		assert.Equal(t, model.InvoiceDocumentStatusDeleted, current.Status)
		assert.Equal(t, 1, current.DeleteAttempts)
		require.NotNil(t, current.DeletedAt)
	}
	current := model.InvoiceDocument{}
	require.NoError(t, db.First(&current, ignoredAvailable.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, current.Status)
	current = model.InvoiceDocument{}
	require.NoError(t, db.First(&current, ignoredDeleting.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusDeleting, current.Status)
}

func TestCleanupInvoiceDocumentsCancellationStopsBeforeClaim(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "old", nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CleanupInvoiceDocuments(ctx, db, store, 100, 50, 10)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, store.deletedKeys)
}

func TestCleanupInvoiceDocumentsRejectsPersistedBucketMismatch(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "old", nil, 1)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Update("r2_bucket", "other-private").Error)
	store.objects["old"] = []byte("pdf")

	_, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)

	require.ErrorIs(t, err, ErrInvoiceObjectTerminal)
	assert.Empty(t, store.deletedKeys)
	var current model.InvoiceDocument
	require.NoError(t, db.First(&current, document.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusSuperseded, current.Status)
}

type invoiceCleanupLeaseLossStore struct {
	*invoiceObjectStoreStub
	db         *gorm.DB
	documentID int64
}

func (store *invoiceCleanupLeaseLossStore) Delete(ctx context.Context, key string) error {
	replacementToken := "another-worker-token"
	if err := store.db.Model(&model.InvoiceDocument{}).Where("id = ?", store.documentID).
		Update("operation_token", replacementToken).Error; err != nil {
		return err
	}
	return store.invoiceObjectStoreStub.Delete(ctx, key)
}

func TestCleanupInvoiceDocumentsStopsWhenDocumentLeaseIsLost(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	baseStore := newInvoiceObjectStoreStub()
	first := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "first", nil, 1)
	seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "second", nil, 1)
	baseStore.objects["first"] = []byte("pdf")
	baseStore.objects["second"] = []byte("pdf")
	store := &invoiceCleanupLeaseLossStore{invoiceObjectStoreStub: baseStore, db: db, documentID: first.ID}

	_, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)
	require.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)
	assert.NotContains(t, baseStore.deletedKeys, "second")
}

func TestCleanupInvoiceDocumentsRecordsSafeTruncatedFailure(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "old", nil, 1)
	store.deleteErrors["old"] = errors.New(strings.Repeat("unsafe detail ", 100))

	result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Failed)
	var current model.InvoiceDocument
	require.NoError(t, db.First(&current, document.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusDeleteFailed, current.Status)
	assert.LessOrEqual(t, len(current.LastDeleteError), 512)
	assert.Equal(t, "object_delete_failed", current.LastDeleteError)
}

func TestInvoiceCleanupHandlerTerminalizesSystemTask(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	store := newInvoiceObjectStoreStub()
	seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "old", nil, 1)
	store.deleteErrors["old"] = ErrInvoiceObjectNotFound
	task, err := model.CreateSystemTask(model.InvoiceDocumentCleanupTaskType, nil, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)

	handler := invoiceDocumentCleanupHandler{db: db, store: store, now: common.GetTimestamp}
	handler.Run(context.Background(), claimed, "runner")

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
}

func TestInvoiceCleanupHandlerReconcilesStaleUploadOperationsWithinBudget(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
	crashAfterCopy := createPromotedInvoiceDocument(t, db, store, 100)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", crashAfterCopy.ID).Update("operation_started_at", 1).Error)
	missingKey := "invoices/missing.pdf"
	missing := seedCleanupDocument(t, db, model.InvoiceDocumentStatusValidating, missingKey, nil, 1)
	stagingKey := "tmp/invoices/missing.pdf"
	uploading := model.InvoiceDocument{
		ApplicationID: 3, R2Bucket: "private", StagingObjectKey: &stagingKey, ContentType: model.InvoicePDFContentType,
		Status: model.InvoiceDocumentStatusUploading, OperationToken: "expired-token", OperationStartedAt: 1,
		UploadedBy: 1, UploadedAt: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&uploading).Error)
	task, err := model.CreateSystemTask(model.InvoiceDocumentCleanupTaskType, nil, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)

	handler := invoiceDocumentCleanupHandler{db: db, store: store, now: common.GetTimestamp}
	handler.Run(context.Background(), claimed, "runner")

	for id, expected := range map[int64]string{
		crashAfterCopy.ID: model.InvoiceDocumentStatusUploadFailed,
		missing.ID:        model.InvoiceDocumentStatusUploadFailed,
		uploading.ID:      model.InvoiceDocumentStatusUploadFailed,
	} {
		var current model.InvoiceDocument
		require.NoError(t, db.First(&current, id).Error)
		assert.Equal(t, expected, current.Status)
	}
	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
}

func TestReconcileStaleInvoiceDocumentsIsolatesRetryableRowsAndSkipsDormantTerminalRows(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "approved", nil)

	retryKey := "tmp/invoices/retry.pdf"
	retry := model.InvoiceDocument{ApplicationID: 1, R2Bucket: "private", StagingObjectKey: &retryKey,
		ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusUploadFailed,
		OperationToken: "retry-token", OperationStartedAt: 1, UploadedBy: 1, UploadedAt: 1,
		RecoveryAttempts: 1, LastRecoveryAt: 1, LastRecoveryError: model.InvoiceDocumentRecoveryDeleteRetryable,
		CreatedAt: 1, UpdatedAt: 1}
	require.NoError(t, db.Create(&retry).Error)
	store.objects[retryKey] = []byte("staging")
	providerSentinel := "provider-error-sentinel-must-not-persist"
	store.deleteErrors[retryKey] = fmt.Errorf("%w: %s", ErrInvoiceObjectRetryable, providerSentinel)

	terminalKey := "tmp/invoices/terminal.pdf"
	terminal := retry
	terminal.ID = 0
	terminal.StagingObjectKey = &terminalKey
	terminal.OperationToken = "terminal-token"
	terminal.LastRecoveryError = model.InvoiceDocumentRecoveryDeleteTerminal
	require.NoError(t, db.Create(&terminal).Error)

	goodKey := "tmp/invoices/good.pdf"
	good := retry
	good.ID = 0
	good.StagingObjectKey = &goodKey
	good.OperationToken = "good-token"
	require.NoError(t, db.Create(&good).Error)
	store.objects[goodKey] = []byte("staging")

	processed, err := ReconcileStaleInvoiceDocuments(context.Background(), db, store, 100, 50, 100)
	require.NoError(t, err)
	assert.Equal(t, 2, processed)

	var retried, dormant, completed model.InvoiceDocument
	require.NoError(t, db.First(&retried, retry.ID).Error)
	require.NoError(t, db.First(&dormant, terminal.ID).Error)
	require.NoError(t, db.First(&completed, good.ID).Error)
	assert.Equal(t, model.InvoiceDocumentRecoveryDeleteRetryable, retried.LastRecoveryError)
	assert.NotContains(t, retried.LastRecoveryError, providerSentinel)
	assert.Equal(t, 2, retried.RecoveryAttempts)
	assert.Equal(t, model.InvoiceDocumentRecoveryDeleteTerminal, dormant.LastRecoveryError)
	assert.Equal(t, 1, dormant.RecoveryAttempts)
	assert.Empty(t, completed.LastRecoveryError)
	assert.NotContains(t, store.objects, goodKey)
}

func TestReconcileStaleInvoiceDocumentsBoundsA501RowBacklog(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
	for index := 0; index < 501; index++ {
		key := fmt.Sprintf("tmp/invoices/%03d.pdf", index)
		document := model.InvoiceDocument{ApplicationID: 1, R2Bucket: "private", StagingObjectKey: &key,
			ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusUploadFailed,
			OperationToken: fmt.Sprintf("token-%03d", index), OperationStartedAt: 1, UploadedBy: 1, UploadedAt: 1,
			RecoveryAttempts: 1, LastRecoveryAt: int64(index + 1), LastRecoveryError: model.InvoiceDocumentRecoveryDeleteRetryable,
			CreatedAt: 1, UpdatedAt: 1}
		require.NoError(t, db.Create(&document).Error)
		store.objects[key] = []byte("staging")
	}

	processed, err := ReconcileStaleInvoiceDocuments(context.Background(), db, store, 1000, 900, 100)
	require.NoError(t, err)
	assert.Equal(t, 100, processed)
	var remaining int64
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("last_recovery_error = ?", model.InvoiceDocumentRecoveryDeleteRetryable).Count(&remaining).Error)
	assert.Equal(t, int64(401), remaining)
}
