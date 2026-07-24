package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceDownloadReadCloser struct {
	io.Reader
	closed bool
	err    error
}

func (reader *invoiceDownloadReadCloser) Close() error {
	reader.closed = true
	return reader.err
}

type invoiceDownloadStoreStub struct {
	bucket         string
	authority      string
	head           InvoiceObjectHead
	headErr        error
	get            InvoiceObjectGet
	getErr         error
	headCalls      int
	getCalls       int
	deleteCalls    int
	getKey         string
	getIfMatch     string
	getBodyFactory func() io.ReadCloser
	beforeHead     func()
}

func (store *invoiceDownloadStoreStub) Bucket() string      { return store.bucket }
func (store *invoiceDownloadStoreStub) AuthorityID() string { return store.authority }
func (*invoiceDownloadStoreStub) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}
func (*invoiceDownloadStoreStub) Copy(context.Context, string, string) error { return nil }
func (store *invoiceDownloadStoreStub) Head(context.Context, string) (InvoiceObjectHead, error) {
	store.headCalls++
	if store.beforeHead != nil {
		store.beforeHead()
	}
	return store.head, store.headErr
}
func (store *invoiceDownloadStoreStub) Get(_ context.Context, key, ifMatch string) (InvoiceObjectGet, error) {
	store.getCalls++
	store.getKey = key
	store.getIfMatch = ifMatch
	if store.getBodyFactory != nil {
		store.get.Body = store.getBodyFactory()
	}
	return store.get, store.getErr
}
func (store *invoiceDownloadStoreStub) Delete(context.Context, string) error {
	store.deleteCalls++
	return nil
}
func setupInvoiceDownloadTest(t *testing.T, application *model.InvoiceApplication, document *model.InvoiceDocument) *invoiceDownloadStoreStub {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}, &model.InvoiceDocument{}))
	require.NoError(t, db.Create(application).Error)
	if document != nil {
		document.ApplicationID = application.ID
		require.NoError(t, db.Create(document).Error)
		if application.ActiveDocumentID == nil {
			application.ActiveDocumentID = &document.ID
		}
		require.NoError(t, db.Model(application).Update("active_document_id", application.ActiveDocumentID).Error)
	}
	previousDB := model.DB
	previousNow := invoiceDownloadNow
	previousFactory := newInvoiceDownloadStore
	model.DB = db
	pdf := []byte("invoice-pdf")
	digest := sha256.Sum256(pdf)
	body := &invoiceDownloadReadCloser{Reader: bytes.NewReader(pdf)}
	store := &invoiceDownloadStoreStub{
		bucket: "private", authority: invoiceTestAuthorityID,
		head: InvoiceObjectHead{SizeBytes: int64(len(pdf)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), ETag: `"opaque-etag"`},
		get:  InvoiceObjectGet{Body: body, SizeBytes: int64(len(pdf)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), ETag: `"opaque-etag"`},
	}
	invoiceDownloadNow = func() time.Time { return time.Unix(1_000, 0) }
	newInvoiceDownloadStore = func() (InvoiceObjectStore, error) { return store, nil }
	t.Cleanup(func() {
		model.DB = previousDB
		invoiceDownloadNow = previousNow
		newInvoiceDownloadStore = previousFactory
	})
	return store
}

func downloadableInvoiceFixture() (model.InvoiceApplication, *model.InvoiceDocument) {
	expiresAt := int64(1_600)
	objectKey := "invoices/random.pdf"
	digest := sha256.Sum256([]byte("invoice-pdf"))
	authority := invoiceTestAuthorityID
	etag := `"opaque-etag"`
	application := model.InvoiceApplication{
		ApplicationNo: "INV-DOWNLOAD", UserID: 11, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusIssued,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	document := &model.InvoiceDocument{
		ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusAvailable,
		R2AuthorityID: &authority, R2Bucket: "private", ObjectKey: &objectKey, ObjectETag: &etag,
		ExpiresAt: &expiresAt, OperationToken: "token", UploadedBy: 1,
		SizeBytes: int64(len("invoice-pdf")), SHA256: hex.EncodeToString(digest[:]),
	}
	return application, document
}

func TestGetInvoiceDocumentDownloadReturnsOnlyVerifiedConditionalBytes(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	store := setupInvoiceDownloadTest(t, &application, document)

	content, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.NoError(t, err)
	assert.Equal(t, []byte("invoice-pdf"), content)
	assert.Zero(t, store.headCalls)
	assert.Equal(t, 1, store.getCalls)
	assert.Equal(t, "invoices/random.pdf", store.getKey)
	assert.Equal(t, `"opaque-etag"`, store.getIfMatch)
	assert.True(t, store.get.Body.(*invoiceDownloadReadCloser).closed)
}

func TestGetInvoiceDocumentDownloadAcceptsBlankProviderChecksumAfterLocalVerification(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	store := setupInvoiceDownloadTest(t, &application, document)
	store.get.ChecksumSHA256 = ""

	content, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.NoError(t, err)
	assert.Equal(t, []byte("invoice-pdf"), content)
	assert.True(t, store.get.Body.(*invoiceDownloadReadCloser).closed)
}

func TestGetInvoiceDocumentDownloadIgnoresCloseFailureAfterVerifiedRead(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	store := setupInvoiceDownloadTest(t, &application, document)
	store.get.Body.(*invoiceDownloadReadCloser).err = errors.New("close failed")

	content, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.NoError(t, err)
	assert.Equal(t, []byte("invoice-pdf"), content)
	assert.True(t, store.get.Body.(*invoiceDownloadReadCloser).closed)
}

func TestGetInvoiceDocumentDownloadRequiresExactAuthorityAndBucketBeforeObjectIO(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*model.InvoiceDocument, *invoiceDownloadStoreStub)
	}{
		{name: "authority mismatch", mutate: func(document *model.InvoiceDocument, store *invoiceDownloadStoreStub) {
			other := strings.Repeat("b", 64)
			document.R2AuthorityID = &other
		}},
		{name: "bucket mismatch", mutate: func(_ *model.InvoiceDocument, store *invoiceDownloadStoreStub) { store.bucket = "other" }},
		{name: "authority unbound", mutate: func(document *model.InvoiceDocument, _ *invoiceDownloadStoreStub) { document.R2AuthorityID = nil }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("INVOICE_R2_LEGACY_AUTHORITY_ID", "")
			application, document := downloadableInvoiceFixture()
			store := setupInvoiceDownloadTest(t, &application, document)
			testCase.mutate(document, store)
			require.NoError(t, model.DB.Model(&model.InvoiceDocument{}).Where("id = ?", *application.ActiveDocumentID).
				Updates(map[string]any{"r2_authority_id": document.R2AuthorityID, "r2_bucket": document.R2Bucket}).Error)

			_, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

			require.Error(t, err)
			assert.Zero(t, store.headCalls)
			assert.Zero(t, store.getCalls)
			assert.Zero(t, store.deleteCalls)
		})
	}
}

func TestGetInvoiceDocumentDownloadBindsExplicitlyAttestedLegacyFacts(t *testing.T) {
	t.Setenv("INVOICE_R2_LEGACY_AUTHORITY_ID", invoiceTestAuthorityID)
	application, document := downloadableInvoiceFixture()
	document.R2AuthorityID = nil
	document.ObjectETag = nil
	store := setupInvoiceDownloadTest(t, &application, document)
	store.getBodyFactory = func() io.ReadCloser {
		return &invoiceDownloadReadCloser{Reader: bytes.NewReader([]byte("invoice-pdf"))}
	}

	content, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.NoError(t, err)
	assert.Equal(t, []byte("invoice-pdf"), content)
	assert.Equal(t, 1, store.headCalls)
	assert.Equal(t, 2, store.getCalls)
	var persisted model.InvoiceDocument
	require.NoError(t, model.DB.First(&persisted, *application.ActiveDocumentID).Error)
	require.NotNil(t, persisted.R2AuthorityID)
	assert.Equal(t, invoiceTestAuthorityID, *persisted.R2AuthorityID)
	require.NotNil(t, persisted.ObjectETag)
	assert.Equal(t, `"opaque-etag"`, *persisted.ObjectETag)
}

func TestGetInvoiceDocumentDownloadBindsLegacyObjectWithBlankProviderChecksum(t *testing.T) {
	t.Setenv("INVOICE_R2_LEGACY_AUTHORITY_ID", invoiceTestAuthorityID)
	application, document := downloadableInvoiceFixture()
	document.R2AuthorityID = nil
	document.ObjectETag = nil
	store := setupInvoiceDownloadTest(t, &application, document)
	store.head.ChecksumSHA256 = ""
	store.get.ChecksumSHA256 = ""
	store.getBodyFactory = func() io.ReadCloser {
		return &invoiceDownloadReadCloser{Reader: bytes.NewReader([]byte("invoice-pdf"))}
	}

	content, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.NoError(t, err)
	assert.Equal(t, []byte("invoice-pdf"), content)
	assert.Equal(t, 1, store.headCalls)
	assert.Equal(t, 2, store.getCalls)
	var persisted model.InvoiceDocument
	require.NoError(t, model.DB.First(&persisted, *application.ActiveDocumentID).Error)
	require.NotNil(t, persisted.R2AuthorityID)
	require.NotNil(t, persisted.ObjectETag)
}

func TestGetInvoiceDocumentDownloadLegacyMismatchRemainsDormant(t *testing.T) {
	t.Setenv("INVOICE_R2_LEGACY_AUTHORITY_ID", invoiceTestAuthorityID)
	application, document := downloadableInvoiceFixture()
	document.R2AuthorityID = nil
	document.ObjectETag = nil
	store := setupInvoiceDownloadTest(t, &application, document)
	store.head.ChecksumSHA256 = "different"

	_, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.ErrorIs(t, err, ErrInvoiceDocumentUnavailable)
	assert.Equal(t, 1, store.headCalls)
	assert.Zero(t, store.getCalls)
	var persisted model.InvoiceDocument
	require.NoError(t, model.DB.First(&persisted, *application.ActiveDocumentID).Error)
	assert.Nil(t, persisted.R2AuthorityID)
	assert.Nil(t, persisted.ObjectETag)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, persisted.Status)
}

func TestGetInvoiceDocumentDownloadOnlyConfirmedAbsenceMarksMissing(t *testing.T) {
	readFailure := errors.New("interrupted read")
	tests := []struct {
		name        string
		mutateStore func(*invoiceDownloadStoreStub)
		wantMissing bool
	}{
		{name: "confirmed absence", wantMissing: true, mutateStore: func(store *invoiceDownloadStoreStub) { store.getErr = ErrInvoiceObjectNotFound }},
		{name: "precondition failed", mutateStore: func(store *invoiceDownloadStoreStub) { store.getErr = ErrInvoiceObjectIntegrityUnavailable }},
		{name: "etag mismatch", mutateStore: func(store *invoiceDownloadStoreStub) { store.get.ETag = `"replacement"` }},
		{name: "checksum mismatch", mutateStore: func(store *invoiceDownloadStoreStub) { store.get.ChecksumSHA256 = "different" }},
		{name: "response size mismatch", mutateStore: func(store *invoiceDownloadStoreStub) { store.get.SizeBytes++ }},
		{name: "overflow", mutateStore: func(store *invoiceDownloadStoreStub) {
			store.get.Body = &invoiceDownloadReadCloser{Reader: strings.NewReader(strings.Repeat("x", int(InvoicePDFMaxBytes+1)))}
			store.get.SizeBytes = InvoicePDFMaxBytes + 1
		}},
		{name: "read failure", mutateStore: func(store *invoiceDownloadStoreStub) {
			store.get.Body = &invoiceDownloadReadCloser{Reader: io.MultiReader(strings.NewReader("invoice"), errorReader{err: readFailure})}
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			application, document := downloadableInvoiceFixture()
			store := setupInvoiceDownloadTest(t, &application, document)
			testCase.mutateStore(store)

			content, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

			require.Error(t, err)
			assert.Nil(t, content)
			var persisted model.InvoiceDocument
			require.NoError(t, model.DB.First(&persisted, *application.ActiveDocumentID).Error)
			if testCase.wantMissing {
				assert.Equal(t, model.InvoiceDocumentStatusMissing, persisted.Status)
			} else {
				assert.Equal(t, model.InvoiceDocumentStatusAvailable, persisted.Status)
			}
			if store.getErr == nil && store.get.Body != nil {
				assert.True(t, store.get.Body.(*invoiceDownloadReadCloser).closed)
			}
		})
	}
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

func TestGetInvoiceDocumentDownloadEnforcesOwnerAndLifecycle(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	store := setupInvoiceDownloadTest(t, &application, document)

	_, err := GetInvoiceDocumentDownload(context.Background(), 12, application.ID)

	require.ErrorIs(t, err, model.ErrInvoiceNotFound)
	assert.Zero(t, store.getCalls)

	require.NoError(t, model.DB.Model(&model.InvoiceApplication{}).Where("id = ?", application.ID).
		Update("payment_review_status", constant.InvoicePaymentReviewStatusPostIssueHold).Error)
	_, err = GetInvoiceDocumentDownload(context.Background(), 11, application.ID)
	require.ErrorIs(t, err, ErrInvoiceDocumentUnavailable)
	assert.Zero(t, store.getCalls)
}
