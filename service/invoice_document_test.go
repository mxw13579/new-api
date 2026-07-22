package service

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceObjectStoreStub struct {
	objects map[string][]byte
}

func newInvoiceObjectStoreStub() *invoiceObjectStoreStub {
	return &invoiceObjectStoreStub{objects: map[string][]byte{}}
}

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
	require.NoError(t, db.AutoMigrate(&model.InvoiceIssuance{}, &model.InvoiceDocument{}))
	return db
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
		document := createPromotedInvoiceDocument(t, db, store, 100)
		lifecycle := NewInvoiceDocumentLifecycle(db, store, validInvoiceDocumentApplicationStub(), 30)
		lifecycle.runTransaction = func(db *gorm.DB, operation func(*gorm.DB) error) error {
			require.NoError(t, db.Transaction(operation))
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

func TestReconcileInvoiceDocumentRefreshesStaleOperationToken(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	document := createPromotedInvoiceDocument(t, db, store, 100)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Update("operation_started_at", 1).Error)

	reconciled, err := ReconcileInvoiceDocument(context.Background(), db, store, document.ID, 50, 60)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusValidating, reconciled.Status)
	assert.NotEqual(t, document.OperationToken, reconciled.OperationToken)

	lifecycle := NewInvoiceDocumentLifecycle(db, store, validInvoiceDocumentApplicationStub(), 30)
	_, err = lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
		ApplicationID: 1, DocumentID: document.ID, OperationToken: document.OperationToken,
		ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
		Issuance: validInvoiceFacts(), PDFFactsAttested: true, AttestedBy: 9, Now: 200,
	})
	assert.ErrorIs(t, err, model.ErrInvoiceDocumentConflict)

	_, err = lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
		ApplicationID: 1, DocumentID: document.ID, OperationToken: reconciled.OperationToken,
		ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
		Issuance: validInvoiceFacts(), PDFFactsAttested: true, AttestedBy: 9, Now: 200,
	})
	require.NoError(t, err)
}
