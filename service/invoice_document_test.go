package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceObjectStoreStub struct {
	objects      map[string][]byte
	deleteErrors map[string]error
	deletedKeys  []string
	bucket       string
}

type invoiceDeleteFailureStore struct {
	*invoiceObjectStoreStub
	failure        error
	db             *gorm.DB
	documentID     int64
	statusAtDelete string
}

func (s *invoiceDeleteFailureStore) Delete(ctx context.Context, key string) error {
	if strings.HasPrefix(key, "tmp/invoices/") && s.failure != nil {
		if s.db != nil && s.documentID > 0 {
			var document model.InvoiceDocument
			if err := s.db.First(&document, s.documentID).Error; err != nil {
				return err
			}
			s.statusAtDelete = document.Status
		}
		return s.failure
	}
	return s.invoiceObjectStoreStub.Delete(ctx, key)
}

func newInvoiceObjectStoreStub() *invoiceObjectStoreStub {
	return &invoiceObjectStoreStub{objects: map[string][]byte{}, deleteErrors: map[string]error{}, bucket: "private"}
}

func (s *invoiceObjectStoreStub) Bucket() string { return s.bucket }

func (s *invoiceObjectStoreStub) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.objects[key] = data
	return nil
}

func (s *invoiceObjectStoreStub) Copy(_ context.Context, sourceKey, destinationKey string) error {
	data, ok := s.objects[sourceKey]
	if !ok {
		return ErrInvoiceObjectNotFound
	}
	s.objects[destinationKey] = append([]byte(nil), data...)
	return nil
}

func (s *invoiceObjectStoreStub) Head(_ context.Context, key string) (InvoiceObjectHead, error) {
	data, ok := s.objects[key]
	if !ok {
		return InvoiceObjectHead{}, ErrInvoiceObjectNotFound
	}
	validation, err := ValidateInvoicePDF(bytes.NewReader(data))
	if err != nil {
		return InvoiceObjectHead{}, err
	}
	checksum, err := invoiceObjectChecksum(validation.SHA256)
	if err != nil {
		return InvoiceObjectHead{}, err
	}
	return InvoiceObjectHead{SizeBytes: int64(len(data)), ChecksumSHA256: checksum}, nil
}

func (s *invoiceObjectStoreStub) Delete(_ context.Context, key string) error {
	s.deletedKeys = append(s.deletedKeys, key)
	if err := s.deleteErrors[key]; err != nil {
		return err
	}
	if _, ok := s.objects[key]; !ok {
		return ErrInvoiceObjectNotFound
	}
	delete(s.objects, key)
	return nil
}

func (s *invoiceObjectStoreStub) PresignGet(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", nil
}

type invoiceDocumentApplicationStub struct {
	prepare       model.PrepareInvoiceDocumentResult
	finalize      model.FinalizeInvoiceDocumentResult
	replace       model.ReplaceInvoiceDocumentResult
	err           error
	finalizeCalls int
	replaceCalls  int
}

func (s *invoiceDocumentApplicationStub) PrepareDocumentTx(*gorm.DB, model.PrepareInvoiceDocumentRequest) (model.PrepareInvoiceDocumentResult, error) {
	return s.prepare, s.err
}

func (s *invoiceDocumentApplicationStub) FinalizeDocumentTx(*gorm.DB, model.FinalizeInvoiceDocumentRequest) (model.FinalizeInvoiceDocumentResult, error) {
	s.finalizeCalls++
	return s.finalize, s.err
}

func (s *invoiceDocumentApplicationStub) ReplaceDocumentTx(*gorm.DB, model.ReplaceInvoiceDocumentRequest) (model.ReplaceInvoiceDocumentResult, error) {
	s.replaceCalls++
	return s.replace, s.err
}

func (s *invoiceDocumentApplicationStub) RevokeDocumentTx(*gorm.DB, model.RevokeInvoiceDocumentRequest) error {
	return s.err
}

func openInvoiceDocumentServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}, &model.InvoiceIssuance{}, &model.InvoiceDocument{}))
	return db
}

func seedInvoiceDocumentApplication(t *testing.T, db *gorm.DB, id int64, status string, activeDocumentID *int64) {
	t.Helper()
	require.NoError(t, db.Create(&model.InvoiceApplication{
		ID: id, ApplicationNo: fmt.Sprintf("APP-%d", id), UserID: int(id), RequestID: fmt.Sprintf("REQ-%d", id),
		RequestFingerprint: strings.Repeat("a", 64), Type: "personal", Status: status,
		PaymentReviewStatus: "none", Currency: "CNY", AmountMinor: 100, FeeMethod: "wallet_quota",
		FeeStatus: "not_required", ProfileSnapshot: `{"title":"Buyer"}`, PolicySnapshot: `{}`,
		ActiveDocumentID: activeDocumentID, SubmittedAt: 1,
	}).Error)
}

func validInvoiceDocumentApplicationStub() *invoiceDocumentApplicationStub {
	return &invoiceDocumentApplicationStub{
		prepare: model.PrepareInvoiceDocumentResult{
			UserID: 1, ApplicationNo: "APP-1", ProfileSnapshot: `{"title":"Buyer"}`,
			AmountMinor: 100, Currency: "CNY",
		},
		finalize: model.FinalizeInvoiceDocumentResult{IssuanceID: 101, ActiveDocumentID: 1, ApplicationStatus: "issued"},
	}
}

func validInvoiceFacts() model.InvoiceIssuanceFacts {
	return model.InvoiceIssuanceFacts{InvoiceNumber: "INV-1", InvoiceDate: 100, FaceAmountMinor: 100, Currency: "CNY"}
}

func createPromotedInvoiceDocument(t *testing.T, db *gorm.DB, store *invoiceObjectStoreStub, now int64) *model.InvoiceDocument {
	t.Helper()
	document, err := CreateInvoiceDocumentUpload(db, "private", 1, 9, now)
	require.NoError(t, err)
	document, err = PromoteInvoiceDocument(context.Background(), db, store, document.ID, document.OperationToken, bytes.NewReader(buildInvoiceTestPDF(t, "")), now+1)
	require.NoError(t, err)
	require.Equal(t, model.InvoiceDocumentStatusValidating, document.Status)
	require.NotNil(t, document.ObjectKey)
	require.Contains(t, store.objects, *document.ObjectKey)
	return document
}

func TestCreateInvoiceDocumentUploadPersistsRandomPrivateKeys(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	first, err := CreateInvoiceDocumentUpload(db, "private", 1, 9, 100)
	require.NoError(t, err)
	second, err := CreateInvoiceDocumentUpload(db, "private", 1, 9, 100)
	require.NoError(t, err)

	require.NotNil(t, first.StagingObjectKey)
	require.NotNil(t, second.StagingObjectKey)
	assert.NotEqual(t, *first.StagingObjectKey, *second.StagingObjectKey)
	assert.NotEqual(t, first.OperationToken, second.OperationToken)
	assert.Regexp(t, `^tmp/invoices/[a-f0-9]{32}\.pdf$`, *first.StagingObjectKey)
	assert.Nil(t, first.ObjectKey)

	var persisted model.InvoiceDocument
	require.NoError(t, db.First(&persisted, first.ID).Error)
	assert.Equal(t, first.OperationToken, persisted.OperationToken)
	assert.Equal(t, first.StagingObjectKey, persisted.StagingObjectKey)

	store := newInvoiceObjectStoreStub()
	first, err = PromoteInvoiceDocument(context.Background(), db, store, first.ID, first.OperationToken, bytes.NewReader(buildInvoiceTestPDF(t, "")), 101)
	require.NoError(t, err)
	second, err = PromoteInvoiceDocument(context.Background(), db, store, second.ID, second.OperationToken, bytes.NewReader(buildInvoiceTestPDF(t, "")), 101)
	require.NoError(t, err)
	require.NotNil(t, first.ObjectKey)
	require.NotNil(t, second.ObjectKey)
	assert.Regexp(t, `^invoices/[a-f0-9]{32}\.pdf$`, *first.ObjectKey)
	assert.NotEqual(t, *first.ObjectKey, *second.ObjectKey)
}

func TestPromoteInvoiceDocumentRequiresStagingDeletionBeforeFinalize(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		failure  error
		status   string
		category string
	}{
		{name: "retryable", failure: ErrInvoiceObjectRetryable, status: model.InvoiceDocumentStatusValidating, category: model.InvoiceDocumentRecoveryDeleteRetryable},
		{name: "terminal", failure: ErrInvoiceObjectTerminal, status: model.InvoiceDocumentStatusUploadFailed, category: model.InvoiceDocumentRecoveryDeleteTerminal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openInvoiceDocumentServiceTestDB(t)
			base := newInvoiceObjectStoreStub()
			store := &invoiceDeleteFailureStore{invoiceObjectStoreStub: base, failure: testCase.failure}
			document, err := CreateInvoiceDocumentUpload(db, "private", 1, 9, 100)
			require.NoError(t, err)
			store.db = db
			store.documentID = document.ID

			_, err = PromoteInvoiceDocument(context.Background(), db, store, document.ID, document.OperationToken, bytes.NewReader(buildInvoiceTestPDF(t, "")), 101)
			require.ErrorIs(t, err, testCase.failure)
			assert.Equal(t, model.InvoiceDocumentStatusUploading, store.statusAtDelete)
			var current model.InvoiceDocument
			require.NoError(t, db.First(&current, document.ID).Error)
			assert.Equal(t, testCase.status, current.Status)
			assert.Equal(t, testCase.category, current.LastRecoveryError)
			assert.Equal(t, 1, current.RecoveryAttempts)

			lifecycle := NewInvoiceDocumentLifecycle(db, store, validInvoiceDocumentApplicationStub(), 30)
			_, finalizeErr := lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
				ApplicationID: 1, DocumentID: current.ID, OperationToken: current.OperationToken,
				ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none", Issuance: validInvoiceFacts(),
				PDFFactsAttested: true, AttestedBy: 9, Now: 200,
			})
			require.ErrorIs(t, finalizeErr, model.ErrInvoiceDocumentConflict)
		})
	}
}

func TestActivateInvoiceDocumentRejectsExpiryOverflowWithoutWrites(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	document := createPromotedInvoiceDocument(t, db, store, 100)
	lifecycle := NewInvoiceDocumentLifecycle(db, store, validInvoiceDocumentApplicationStub(), 1)

	_, err := lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
		ApplicationID: 1, DocumentID: document.ID, OperationToken: document.OperationToken,
		ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none", Issuance: validInvoiceFacts(),
		PDFFactsAttested: true, AttestedBy: 9, Now: int64(^uint64(0) >> 1),
	})
	require.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)
	var current model.InvoiceDocument
	require.NoError(t, db.First(&current, document.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusUploadFailed, current.Status)
	assert.Nil(t, current.ExpiresAt)
}

func TestInvoiceDocumentExpiryChecksExactArithmeticBoundary(t *testing.T) {
	now := int64(100)
	maxDays := int((int64(^uint64(0)>>1) - now) / 86400)
	expiresAt, err := invoiceDocumentExpiry(now, maxDays)
	require.NoError(t, err)
	assert.Equal(t, now+int64(maxDays)*86400, expiresAt)

	_, err = invoiceDocumentExpiry(now, maxDays+1)
	require.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)
	_, err = invoiceDocumentExpiry(int64(^uint64(0)>>1), 1)
	require.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)
}

func TestInvoiceDocumentCrashAfterCopyCanFinalizeWithPerVersionAttestation(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	document := createPromotedInvoiceDocument(t, db, store, 100)
	application := validInvoiceDocumentApplicationStub()
	lifecycle := NewInvoiceDocumentLifecycle(db, store, application, 30)

	result, err := lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
		ApplicationID: 1, DocumentID: document.ID, OperationToken: document.OperationToken,
		ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
		Issuance: validInvoiceFacts(), PDFFactsAttested: true, AttestedBy: 9, Now: 200,
	})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, result.Status)
	assert.Equal(t, 1, application.finalizeCalls)
	require.NotNil(t, result.AttestedAt)
	assert.Equal(t, int64(200), *result.AttestedAt)
	assert.True(t, result.PDFFactsAttested)
	assert.Equal(t, 9, *result.AttestedBy)
	assert.Len(t, result.AttestedProfileSnapshotSHA256, 64)
	assert.Equal(t, 30, result.RetentionDaysSnapshot)
	require.NotNil(t, result.ExpiresAt)
	assert.Equal(t, int64(200+30*86400), *result.ExpiresAt)
}

func TestInvoiceDocumentFinalizeDistinguishesRollbackFromAmbiguousCommit(t *testing.T) {
	t.Run("definite rollback deletes copied object", func(t *testing.T) {
		db := openInvoiceDocumentServiceTestDB(t)
		store := newInvoiceObjectStoreStub()
		document := createPromotedInvoiceDocument(t, db, store, 100)
		application := validInvoiceDocumentApplicationStub()
		application.err = model.ErrInvoiceStateConflict
		lifecycle := NewInvoiceDocumentLifecycle(db, store, application, 30)

		_, err := lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
			ApplicationID: 1, DocumentID: document.ID, OperationToken: document.OperationToken,
			ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
			Issuance: validInvoiceFacts(), PDFFactsAttested: true, AttestedBy: 9, Now: 200,
		})
		require.ErrorIs(t, err, model.ErrInvoiceStateConflict)
		assert.NotContains(t, store.objects, *document.ObjectKey)

		var failed model.InvoiceDocument
		require.NoError(t, db.First(&failed, document.ID).Error)
		assert.Equal(t, model.InvoiceDocumentStatusUploadFailed, failed.Status)
	})

	t.Run("ambiguous committed outcome is reread and retained", func(t *testing.T) {
		db := openInvoiceDocumentServiceTestDB(t)
		store := newInvoiceObjectStoreStub()
		seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
		document := createPromotedInvoiceDocument(t, db, store, 100)
		lifecycle := NewInvoiceDocumentLifecycle(db, store, validInvoiceDocumentApplicationStub(), 30)
		lifecycle.runTransaction = func(db *gorm.DB, operation func(*gorm.DB) error) error {
			require.NoError(t, db.Transaction(operation))
			require.NoError(t, db.Model(&model.InvoiceApplication{}).Where("id = ?", 1).Updates(map[string]any{
				"status": "issued", "active_document_id": document.ID,
			}).Error)
			return ErrInvoiceCommitAmbiguous
		}

		result, err := lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
			ApplicationID: 1, DocumentID: document.ID, OperationToken: document.OperationToken,
			ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
			Issuance: validInvoiceFacts(), PDFFactsAttested: true, AttestedBy: 9, Now: 200,
		})
		require.NoError(t, err)
		assert.Equal(t, model.InvoiceDocumentStatusAvailable, result.Status)
		assert.Contains(t, store.objects, *document.ObjectKey)
	})

	t.Run("ambiguous non-commit proven by reread deletes object", func(t *testing.T) {
		db := openInvoiceDocumentServiceTestDB(t)
		store := newInvoiceObjectStoreStub()
		seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
		document := createPromotedInvoiceDocument(t, db, store, 100)
		lifecycle := NewInvoiceDocumentLifecycle(db, store, validInvoiceDocumentApplicationStub(), 30)
		lifecycle.runTransaction = func(*gorm.DB, func(*gorm.DB) error) error { return ErrInvoiceCommitAmbiguous }

		_, err := lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
			ApplicationID: 1, DocumentID: document.ID, OperationToken: document.OperationToken,
			ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
			Issuance: validInvoiceFacts(), PDFFactsAttested: true, AttestedBy: 9, Now: 200,
		})
		require.ErrorIs(t, err, ErrInvoiceCommitAmbiguous)
		assert.NotContains(t, store.objects, *document.ObjectKey)
	})
}

func TestInvoiceDocumentReplacementRequiresFreshAttestation(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	application := validInvoiceDocumentApplicationStub()
	lifecycle := NewInvoiceDocumentLifecycle(db, store, application, 30)
	first := createPromotedInvoiceDocument(t, db, store, 100)
	first, err := lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
		ApplicationID: 1, DocumentID: first.ID, OperationToken: first.OperationToken,
		ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
		Issuance: validInvoiceFacts(), PDFFactsAttested: true, AttestedBy: 9, Now: 200,
	})
	require.NoError(t, err)
	firstAttestedAt := *first.AttestedAt

	second := createPromotedInvoiceDocument(t, db, store, 300)
	_, err = lifecycle.Replace(context.Background(), ReplaceInvoiceDocumentOperation{
		ApplicationID: 1, NewDocumentID: second.ID, OperationToken: second.OperationToken,
		ExpectedStatus: "issued", ExpectedPaymentReviewStatus: "none",
		ExpectedActiveDocumentID: first.ID, ExpectedIssuanceID: *first.IssuanceID,
		PDFFactsAttested: false, AttestedBy: 10, Now: 400,
	})
	require.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)

	application.replace = model.ReplaceInvoiceDocumentResult{SupersededDocumentID: first.ID, ActiveDocumentID: second.ID}
	replaced, err := lifecycle.Replace(context.Background(), ReplaceInvoiceDocumentOperation{
		ApplicationID: 1, NewDocumentID: second.ID, OperationToken: second.OperationToken,
		ExpectedStatus: "issued", ExpectedPaymentReviewStatus: "none",
		ExpectedActiveDocumentID: first.ID, ExpectedIssuanceID: *first.IssuanceID,
		PDFFactsAttested: true, AttestedBy: 10, Now: 400,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), *replaced.Version)
	assert.Equal(t, 10, *replaced.AttestedBy)
	assert.Equal(t, int64(400), *replaced.AttestedAt)

	var unchangedFirst model.InvoiceDocument
	require.NoError(t, db.First(&unchangedFirst, first.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusSuperseded, unchangedFirst.Status)
	assert.Equal(t, 9, *unchangedFirst.AttestedBy)
	assert.Equal(t, firstAttestedAt, *unchangedFirst.AttestedAt)
}

func TestReconcileInvoiceDocumentTerminalizesCrashAfterCopyWhenActivationDidNotCommit(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
	document := createPromotedInvoiceDocument(t, db, store, 100)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Update("operation_started_at", 1).Error)

	reconciled, err := ReconcileInvoiceDocument(context.Background(), db, store, document.ID, 50, 60)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusUploadFailed, reconciled.Status)
	assert.NotEqual(t, document.OperationToken, reconciled.OperationToken)
	assert.NotContains(t, store.objects, *document.ObjectKey)
}

func TestReconcileInvoiceDocumentNeverDeletesWhenApplicationPointsAtDocument(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "issued", nil)
	document := createPromotedInvoiceDocument(t, db, store, 100)
	require.NoError(t, db.Model(&model.InvoiceApplication{}).Where("id = ?", 1).Update("active_document_id", document.ID).Error)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Update("operation_started_at", 1).Error)

	reconciled, err := ReconcileInvoiceDocument(context.Background(), db, store, document.ID, 50, 60)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusUploadFailed, reconciled.Status)
	assert.Equal(t, model.InvoiceDocumentRecoveryActivationIncomplete, reconciled.LastRecoveryError)
	assert.Contains(t, store.objects, *document.ObjectKey)
}

func TestReconcileInvoiceDocumentSafelyReplaysDurableActivationFacts(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "issued", nil)
	document := createPromotedInvoiceDocument(t, db, store, 100)
	issuanceID := int64(10)
	version := int64(1)
	availableAt := int64(20)
	expiresAt := int64(200)
	attestedBy := 9
	attestedAt := int64(20)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Updates(map[string]any{
		"issuance_id": issuanceID, "version": version, "pdf_facts_attested": true, "attested_by": attestedBy,
		"attested_at": attestedAt, "attested_profile_snapshot_sha256": strings.Repeat("a", 64),
		"available_at": availableAt, "retention_days_snapshot": 30, "expires_at": expiresAt, "operation_started_at": 1,
	}).Error)
	require.NoError(t, db.Model(&model.InvoiceApplication{}).Where("id = ?", 1).Update("active_document_id", document.ID).Error)
	require.NotNil(t, document.StagingObjectKey)
	store.objects[*document.StagingObjectKey] = []byte("residual staging")

	reconciled, err := ReconcileInvoiceDocument(context.Background(), db, store, document.ID, 50, 60)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, reconciled.Status)
	assert.Contains(t, store.objects, *document.ObjectKey)
	assert.NotContains(t, store.objects, *document.StagingObjectKey)
	assert.Empty(t, reconciled.LastRecoveryError)
}

func TestReconcileInvoiceDocumentPersistsDormantBucketMismatch(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, "approved", nil)
	document := createPromotedInvoiceDocument(t, db, store, 100)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Updates(map[string]any{
		"r2_bucket": "another-private-bucket", "operation_started_at": 1,
	}).Error)

	reconciled, err := ReconcileInvoiceDocument(context.Background(), db, store, document.ID, 50, 60)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusUploadFailed, reconciled.Status)
	assert.Equal(t, model.InvoiceDocumentRecoveryBucketMismatch, reconciled.LastRecoveryError)
	assert.Contains(t, store.objects, *document.ObjectKey)

	_, err = ReconcileInvoiceDocument(context.Background(), db, store, document.ID, 100, 90)
	require.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)
}

func TestInvoiceTransactionRunnerClassifiesCommitErrorAsAmbiguous(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	bodyCalled := false
	err := runInvoiceDocumentTransaction(db, func(tx *gorm.DB) error {
		bodyCalled = true
		return tx.Create(&model.InvoiceIssuance{
			ApplicationID: 999, InvoiceNumber: "AMBIGUOUS-COMMIT", InvoiceDate: 1, FaceAmountMinor: 1,
			Currency: "CNY", CreatedBy: 1, CreatedAt: 1, UpdatedAt: 1,
		}).Error
	}, func(tx *gorm.DB) error {
		require.NoError(t, tx.Commit().Error)
		return errors.New("commit response lost")
	})
	require.ErrorIs(t, err, ErrInvoiceCommitAmbiguous)
	assert.True(t, bodyCalled)
	var committed model.InvoiceIssuance
	require.NoError(t, db.Where("application_id = ?", 999).First(&committed).Error)
}

func TestInvoiceTransactionRunnerRollsBackPanicAndRepanics(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	panicValue := "invoice transaction panic"
	func() {
		defer func() { assert.Equal(t, panicValue, recover()) }()
		_ = runInvoiceDocumentTransaction(db, func(tx *gorm.DB) error {
			require.NoError(t, tx.Create(&model.InvoiceIssuance{
				ApplicationID: 777, InvoiceNumber: "PANIC-ROLLBACK", InvoiceDate: 1, FaceAmountMinor: 1,
				Currency: "CNY", CreatedBy: 1, CreatedAt: 1, UpdatedAt: 1,
			}).Error)
			panic(panicValue)
		}, nil)
	}()
	var count int64
	require.NoError(t, db.Model(&model.InvoiceIssuance{}).Where("application_id = ?", 777).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Create(&model.InvoiceIssuance{
		ApplicationID: 778, InvoiceNumber: "AFTER-PANIC", InvoiceDate: 1, FaceAmountMinor: 1,
		Currency: "CNY", CreatedBy: 1, CreatedAt: 1, UpdatedAt: 1,
	}).Error)
}
