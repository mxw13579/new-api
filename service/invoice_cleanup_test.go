package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
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

func TestInvoiceCleanupHandlerReservesHalfBudgetForRetention(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
	for index := 0; index < 501; index++ {
		stagingKey := fmt.Sprintf("tmp/invoices/stale-%03d.pdf", index)
		document := model.InvoiceDocument{
			ApplicationID: 1, R2Bucket: "private", StagingObjectKey: &stagingKey,
			ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusUploadFailed,
			OperationToken: fmt.Sprintf("stale-token-%03d", index), OperationStartedAt: 1,
			UploadedBy: 1, UploadedAt: 1, RecoveryAttempts: 1, LastRecoveryAt: 1,
			LastRecoveryError: model.InvoiceDocumentRecoveryDeleteRetryable, CreatedAt: 1, UpdatedAt: 1,
		}
		require.NoError(t, db.Create(&document).Error)
		store.deleteErrors[stagingKey] = fmt.Errorf("%w: row-local", ErrInvoiceObjectRetryable)
	}
	for index := 0; index < 501; index++ {
		key := fmt.Sprintf("invoices/retention-%03d.pdf", index)
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, key, nil, 1)
		store.objects[key] = []byte("pdf")
	}

	var result InvoiceDocumentCleanupResult
	handler := invoiceDocumentCleanupHandler{
		db: db, store: store, now: func() int64 { return 1000 },
		finish: func(_ string, _ string, _ model.SystemTaskStatus, payload any, _ string) error {
			result = payload.(InvoiceDocumentCleanupResult)
			return nil
		},
	}
	handler.Run(context.Background(), &model.SystemTask{TaskID: "task-fairness"}, "runner")

	assert.Equal(t, 50, result.Reconciled)
	assert.Equal(t, 50, result.Processed)
	assert.Equal(t, 50, result.Deleted)
	assert.Len(t, store.deletedKeys, 100)
}

func TestCleanupInvoiceDocumentsEligibilityAndPoisonIsolation(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	expired, future := int64(90), int64(200)
	due, notDue := int64(90), int64(200)
	retryable := model.InvoiceDocumentDeleteErrorRetryable
	terminal := model.InvoiceDocumentDeleteErrorTerminal
	mismatch := model.InvoiceDocumentDeleteErrorObjectIntegrityMismatch

	eligible := []model.InvoiceDocument{
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusAvailable, "invoices/expired.pdf", &expired, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/superseded.pdf", &future, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleting, "invoices/stale.pdf", &future, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleteFailed, "invoices/legacy.pdf", &future, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusMissing, "invoices/mismatch.pdf", &expired, 1),
	}
	retry := seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleteFailed, "invoices/retry.pdf", &future, 1)
	retry.DeleteErrorCategory, retry.NextDeleteAttemptAt = &retryable, &due
	require.NoError(t, db.Save(&retry).Error)
	eligible = append(eligible, retry)

	confirmedAbsent := seedCleanupDocument(t, db, model.InvoiceDocumentStatusMissing, "invoices/absent.pdf", &expired, 1)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", eligible[4].ID).Update("delete_error_category", mismatch).Error)
	dormant := seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleteFailed, "invoices/dormant.pdf", &future, 1)
	dormant.DeleteErrorCategory = &terminal
	require.NoError(t, db.Save(&dormant).Error)
	waiting := seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleteFailed, "invoices/waiting.pdf", &future, 1)
	waiting.DeleteErrorCategory, waiting.NextDeleteAttemptAt = &retryable, &notDue
	require.NoError(t, db.Save(&waiting).Error)
	for _, document := range eligible {
		store.objects[*document.ObjectKey] = []byte("pdf")
	}

	result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 20)
	require.NoError(t, err)
	assert.Equal(t, len(eligible), result.Deleted)
	assert.NotContains(t, store.deletedKeys, *confirmedAbsent.ObjectKey)
	assert.NotContains(t, store.deletedKeys, *dormant.ObjectKey)
	assert.NotContains(t, store.deletedKeys, *waiting.ObjectKey)
}

func TestCleanupInvoiceDocumentsPrevalidationIsTerminalWithoutAttemptAndContinues(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	poisons := []model.InvoiceDocument{
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "", nil, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invalid/key.pdf", nil, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/wrong-bucket.pdf", nil, 1),
	}
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", poisons[0].ID).Update("object_key", nil).Error)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", poisons[2].ID).Update("r2_bucket", "other").Error)
	good := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/good.pdf", nil, 1)
	store.objects[*good.ObjectKey] = []byte("pdf")

	result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)
	require.NoError(t, err)
	assert.Equal(t, 4, result.Processed)
	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, 3, result.Failed)
	assert.Equal(t, []string{"invoices/good.pdf"}, store.deletedKeys)
	for _, poison := range poisons {
		var current model.InvoiceDocument
		require.NoError(t, db.First(&current, poison.ID).Error)
		assert.Equal(t, model.InvoiceDocumentStatusDeleteFailed, current.Status)
		assert.Zero(t, current.DeleteAttempts)
		require.NotNil(t, current.DeleteErrorCategory)
		assert.Nil(t, current.NextDeleteAttemptAt)
	}
}

func TestCleanupInvoiceDocumentsBackoffAndCeiling(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		priorAttempts    int
		now              int64
		expectedAttempt  int
		expectedNext     *int64
		expectedCalls    int
		expectedCategory string
	}{
		{name: "attempt 1", priorAttempts: 0, now: 100, expectedAttempt: 1, expectedNext: int64Pointer(100 + 15*60), expectedCalls: 1, expectedCategory: model.InvoiceDocumentDeleteErrorRetryable},
		{name: "attempt 2", priorAttempts: 1, now: 100, expectedAttempt: 2, expectedNext: int64Pointer(100 + 30*60), expectedCalls: 1, expectedCategory: model.InvoiceDocumentDeleteErrorRetryable},
		{name: "attempt 3", priorAttempts: 2, now: 100, expectedAttempt: 3, expectedNext: int64Pointer(100 + 60*60), expectedCalls: 1, expectedCategory: model.InvoiceDocumentDeleteErrorRetryable},
		{name: "attempt 7", priorAttempts: 6, now: 100, expectedAttempt: 7, expectedNext: int64Pointer(100 + 16*60*60), expectedCalls: 1, expectedCategory: model.InvoiceDocumentDeleteErrorRetryable},
		{name: "attempt 8", priorAttempts: 7, now: 100, expectedAttempt: 8, expectedNext: int64Pointer(100 + 24*60*60), expectedCalls: 1, expectedCategory: model.InvoiceDocumentDeleteErrorRetryable},
		{name: "attempt 16 terminal", priorAttempts: 15, now: 100, expectedAttempt: 16, expectedCalls: 1, expectedCategory: model.InvoiceDocumentDeleteErrorTerminal},
		{name: "legacy ceiling no call", priorAttempts: 16, now: 100, expectedAttempt: 16, expectedCalls: 0, expectedCategory: model.InvoiceDocumentDeleteErrorTerminal},
		{name: "overflow terminal", priorAttempts: 0, now: math.MaxInt64 - 100, expectedAttempt: 1, expectedCalls: 1, expectedCategory: model.InvoiceDocumentDeleteErrorTerminal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openInvoiceDocumentServiceTestDB(t)
			store := newInvoiceObjectStoreStub()
			document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/retry.pdf", nil, 1)
			require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Update("delete_attempts", testCase.priorAttempts).Error)
			store.deleteErrors["invoices/retry.pdf"] = ErrInvoiceObjectRetryable

			_, err := CleanupInvoiceDocuments(context.Background(), db, store, testCase.now, 50, 1)
			require.NoError(t, err)
			var current model.InvoiceDocument
			require.NoError(t, db.First(&current, document.ID).Error)
			assert.Equal(t, testCase.expectedAttempt, current.DeleteAttempts)
			assert.Equal(t, testCase.expectedNext, current.NextDeleteAttemptAt)
			require.NotNil(t, current.DeleteErrorCategory)
			assert.Equal(t, testCase.expectedCategory, *current.DeleteErrorCategory)
			assert.Len(t, store.deletedKeys, testCase.expectedCalls)
		})
	}
}

func TestCleanupInvoiceDocumentsClassifiesDeleteOutcomesAndFencesContext(t *testing.T) {
	for _, testCase := range []struct {
		name, status, category string
		failure                error
	}{
		{name: "no such key", status: model.InvoiceDocumentStatusDeleted, failure: ErrInvoiceObjectNotFound},
		{name: "no such bucket", status: model.InvoiceDocumentStatusDeleteFailed, category: model.InvoiceDocumentDeleteErrorBucketMismatch, failure: ErrInvoiceObjectBucketUnavailable},
		{name: "unknown terminal", status: model.InvoiceDocumentStatusDeleteFailed, category: model.InvoiceDocumentDeleteErrorTerminal, failure: ErrInvoiceObjectTerminal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openInvoiceDocumentServiceTestDB(t)
			store := newInvoiceObjectStoreStub()
			document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/outcome.pdf", nil, 1)
			store.deleteErrors["invoices/outcome.pdf"] = testCase.failure
			_, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 1)
			require.NoError(t, err)
			var current model.InvoiceDocument
			require.NoError(t, db.First(&current, document.ID).Error)
			assert.Equal(t, testCase.status, current.Status)
			if testCase.category == "" {
				assert.Nil(t, current.DeleteErrorCategory)
			} else {
				require.NotNil(t, current.DeleteErrorCategory)
				assert.Equal(t, testCase.category, *current.DeleteErrorCategory)
			}
		})
	}

	db := openInvoiceDocumentServiceTestDB(t)
	base := newInvoiceObjectStoreStub()
	document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/cancel.pdf", nil, 1)
	base.objects["invoices/cancel.pdf"] = []byte("pdf")
	ctx, cancel := context.WithCancel(context.Background())
	store := &invoiceCleanupCancelAfterDeleteStore{invoiceObjectStoreStub: base, cancel: cancel}
	_, err := CleanupInvoiceDocuments(ctx, db, store, 100, 50, 1)
	require.ErrorIs(t, err, context.Canceled)
	var current model.InvoiceDocument
	require.NoError(t, db.First(&current, document.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusDeleting, current.Status)
	assert.Equal(t, 1, current.DeleteAttempts)
}

type invoiceCleanupCancelAfterDeleteStore struct {
	*invoiceObjectStoreStub
	cancel context.CancelFunc
}

type invoiceCleanupClaimObservationStore struct {
	*invoiceObjectStoreStub
	db             *gorm.DB
	documentID     int64
	statusAtCall   string
	attemptsAtCall int
}

func (store *invoiceCleanupClaimObservationStore) Delete(ctx context.Context, key string) error {
	var document model.InvoiceDocument
	if err := store.db.First(&document, store.documentID).Error; err != nil {
		return err
	}
	store.statusAtCall = document.Status
	store.attemptsAtCall = document.DeleteAttempts
	return store.invoiceObjectStoreStub.Delete(ctx, key)
}

func TestCleanupInvoiceDocumentsPersistsAuthorizedAttemptBeforeDelete(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	base := newInvoiceObjectStoreStub()
	document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/claim.pdf", nil, 1)
	base.objects["invoices/claim.pdf"] = []byte("pdf")
	store := &invoiceCleanupClaimObservationStore{invoiceObjectStoreStub: base, db: db, documentID: document.ID}

	result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, model.InvoiceDocumentStatusDeleting, store.statusAtCall)
	assert.Equal(t, 1, store.attemptsAtCall)
}

func (store *invoiceCleanupCancelAfterDeleteStore) Delete(ctx context.Context, key string) error {
	err := store.invoiceObjectStoreStub.Delete(ctx, key)
	store.cancel()
	return err
}

func TestInvoiceCleanupHandlerFinishPersistenceWarningIsSanitized(t *testing.T) {
	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	previousErrorWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousErrorWriter
		common.LogWriterMu.Unlock()
	})

	handler := invoiceDocumentCleanupHandler{
		db: openInvoiceDocumentServiceTestDB(t), store: newInvoiceObjectStoreStub(), now: func() int64 { return 100 },
		finish: func(string, string, model.SystemTaskStatus, any, string) error {
			return errors.New("secret endpoint bucket key credential")
		},
	}
	handler.Run(context.Background(), &model.SystemTask{TaskID: "safe-task-id"}, "runner")
	assert.Contains(t, logs.String(), "safe-task-id")
	assert.NotContains(t, logs.String(), "secret")

	logs.Reset()
	handler.finish = func(string, string, model.SystemTaskStatus, any, string) error { return model.ErrSystemTaskLockLost }
	handler.Run(context.Background(), &model.SystemTask{TaskID: "expected-lock-loss"}, "runner")
	assert.Empty(t, logs.String())
}

func TestCleanupInvoiceDocumentsCoversStatusesLeaseAndNotFound(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	expired := int64(90)
	future := int64(200)
	candidates := []model.InvoiceDocument{
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusAvailable, "invoices/expired.pdf", &expired, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleteFailed, "invoices/retry.pdf", nil, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/old.pdf", nil, 1),
		seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleting, "invoices/stale.pdf", nil, 1),
	}
	ignoredAvailable := seedCleanupDocument(t, db, model.InvoiceDocumentStatusAvailable, "invoices/future.pdf", &future, 1)
	ignoredDeleting := seedCleanupDocument(t, db, model.InvoiceDocumentStatusDeleting, "invoices/fresh.pdf", nil, 99)
	store.objects["invoices/expired.pdf"] = []byte("pdf")
	store.objects["invoices/retry.pdf"] = []byte("pdf")
	store.objects["invoices/old.pdf"] = []byte("pdf")
	store.deleteErrors["invoices/stale.pdf"] = ErrInvoiceObjectNotFound

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
	seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/old.pdf", nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CleanupInvoiceDocuments(ctx, db, store, 100, 50, 10)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, store.deletedKeys)
}

func TestCleanupInvoiceDocumentsRejectsPersistedBucketMismatch(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/old.pdf", nil, 1)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Update("r2_bucket", "other-private").Error)
	store.objects["invoices/old.pdf"] = []byte("pdf")

	result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Failed)
	assert.Empty(t, store.deletedKeys)
	var current model.InvoiceDocument
	require.NoError(t, db.First(&current, document.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusDeleteFailed, current.Status)
	assert.Zero(t, current.DeleteAttempts)
	require.NotNil(t, current.DeleteErrorCategory)
	assert.Equal(t, model.InvoiceDocumentDeleteErrorBucketMismatch, *current.DeleteErrorCategory)
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
	first := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/first.pdf", nil, 1)
	seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/second.pdf", nil, 1)
	baseStore.objects["invoices/first.pdf"] = []byte("pdf")
	baseStore.objects["invoices/second.pdf"] = []byte("pdf")
	store := &invoiceCleanupLeaseLossStore{invoiceObjectStoreStub: baseStore, db: db, documentID: first.ID}

	_, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)
	require.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)
	assert.NotContains(t, baseStore.deletedKeys, "invoices/second.pdf")
}

func TestCleanupInvoiceDocumentsRecordsSafeTruncatedFailure(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	document := seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/old.pdf", nil, 1)
	store.deleteErrors["invoices/old.pdf"] = errors.New(strings.Repeat("unsafe detail ", 100))

	result, err := CleanupInvoiceDocuments(context.Background(), db, store, 100, 50, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Failed)
	var current model.InvoiceDocument
	require.NoError(t, db.First(&current, document.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusDeleteFailed, current.Status)
	assert.LessOrEqual(t, len(current.LastDeleteError), 512)
	assert.Equal(t, model.InvoiceDocumentDeleteErrorTerminal, current.LastDeleteError)
}

func TestInvoiceCleanupHandlerTerminalizesSystemTask(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	store := newInvoiceObjectStoreStub()
	seedCleanupDocument(t, db, model.InvoiceDocumentStatusSuperseded, "invoices/old.pdf", nil, 1)
	store.deleteErrors["invoices/old.pdf"] = ErrInvoiceObjectNotFound
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

func TestInvoiceRecoverySentinelsNeverReachTaskStateOrCapturedLogs(t *testing.T) {
	sentinels := []string{
		"endpoint-sentinel-rw2", "bucket-sentinel-rw2", "access-key-sentinel-rw2",
		"secret-sentinel-rw2", "final-key-sentinel-rw2", "staging-key-sentinel-rw2",
		"provider-text-sentinel-rw2", "filename-sentinel-rw2.pdf", "signed-url-sentinel-rw2",
		"pdf-bytes-sentinel-rw2",
	}
	db := openInvoiceDocumentServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
	finalKey, stagingKey := sentinels[4], sentinels[5]
	document := model.InvoiceDocument{
		ApplicationID: 1, R2Bucket: "private", ObjectKey: &finalKey, StagingObjectKey: &stagingKey,
		ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusValidating,
		OperationToken: "sentinel-operation-token", OperationStartedAt: 1, UploadedBy: 1, UploadedAt: 1,
		CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&document).Error)
	store.objects[finalKey] = []byte(sentinels[9])
	store.objects[stagingKey] = []byte(sentinels[9])
	store.deleteErrors[finalKey] = fmt.Errorf("%w: %s", ErrInvoiceObjectRetryable, strings.Join(sentinels, "|"))

	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = &logs, &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = previousWriter, previousErrorWriter
		common.LogWriterMu.Unlock()
	})

	task, err := model.CreateSystemTask(model.InvoiceDocumentCleanupTaskType, nil, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "sentinel-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	handler := invoiceDocumentCleanupHandler{db: db, store: store, now: func() int64 { return 1000 }}
	handler.Run(context.Background(), claimed, "sentinel-runner")

	var current model.InvoiceDocument
	require.NoError(t, db.First(&current, document.ID).Error)
	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	storedSurface := strings.Join([]string{
		current.LastRecoveryError, current.LastDeleteError,
		finished.Payload, finished.State, finished.Result, finished.Error, logs.String(),
	}, "|")
	for _, sentinel := range sentinels {
		assert.NotContains(t, storedSurface, sentinel)
	}
	assert.Equal(t, model.InvoiceDocumentRecoveryDeleteRetryable, current.LastRecoveryError)
}
