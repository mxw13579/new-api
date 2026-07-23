package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceDownloadStoreStub struct {
	url          string
	ttl          time.Duration
	presignErr   error
	bucket       string
	head         InvoiceObjectHead
	headErr      error
	headCalls    int
	presignCalls int
	operationLog []string
	beforeHead   func()
}

func (s *invoiceDownloadStoreStub) Bucket() string { return s.bucket }

func (s *invoiceDownloadStoreStub) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}
func (s *invoiceDownloadStoreStub) Copy(context.Context, string, string) error { return nil }
func (s *invoiceDownloadStoreStub) Head(context.Context, string) (InvoiceObjectHead, error) {
	s.headCalls++
	s.operationLog = append(s.operationLog, "head")
	if s.beforeHead != nil {
		s.beforeHead()
	}
	return s.head, s.headErr
}
func (s *invoiceDownloadStoreStub) Delete(context.Context, string) error { return nil }
func (s *invoiceDownloadStoreStub) PresignGet(_ context.Context, _ string, ttl time.Duration) (string, error) {
	s.presignCalls++
	s.operationLog = append(s.operationLog, "presign")
	s.ttl = ttl
	return s.url, s.presignErr
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
	digest := sha256.Sum256([]byte("invoice-pdf"))
	store := &invoiceDownloadStoreStub{
		url: "https://signed.example.test/private-token", bucket: "private",
		head: InvoiceObjectHead{SizeBytes: int64(len("invoice-pdf")), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:])},
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
	application := model.InvoiceApplication{
		ApplicationNo: "INV-DOWNLOAD", UserID: 11, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusIssued,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	document := &model.InvoiceDocument{
		ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusAvailable,
		R2Bucket: "private", ObjectKey: &objectKey, ExpiresAt: &expiresAt, OperationToken: "token", UploadedBy: 1,
		SizeBytes: int64(len("invoice-pdf")), SHA256: hex.EncodeToString(digest[:]),
	}
	return application, document
}

func TestGetInvoiceDocumentDownloadRejectsConfiguredBucketMismatch(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	store := setupInvoiceDownloadTest(t, &application, document)
	store.bucket = "wrong-private-bucket"

	_, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.ErrorIs(t, err, ErrInvoiceObjectTerminal)
	assert.Zero(t, store.headCalls)
	assert.Zero(t, store.presignCalls)
}

func TestGetInvoiceDocumentDownloadEnforcesOwnerAndLifecycle(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.InvoiceApplication, *model.InvoiceDocument)
		userID int
		want   error
	}{
		{name: "cross user is masked", userID: 12, want: model.ErrInvoiceNotFound},
		{name: "payment review hold", userID: 11, mutate: func(a *model.InvoiceApplication, _ *model.InvoiceDocument) {
			a.PaymentReviewStatus = constant.InvoicePaymentReviewStatusPostIssueHold
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "application not issued", userID: 11, mutate: func(a *model.InvoiceApplication, _ *model.InvoiceDocument) {
			a.Status = constant.InvoiceApplicationStatusApproved
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "wrong active pointer", userID: 11, mutate: func(a *model.InvoiceApplication, _ *model.InvoiceDocument) {
			wrong := int64(999)
			a.ActiveDocumentID = &wrong
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "document unavailable state", userID: 11, mutate: func(_ *model.InvoiceApplication, d *model.InvoiceDocument) {
			d.Status = model.InvoiceDocumentStatusDeleteFailed
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "missing", userID: 11, mutate: func(_ *model.InvoiceApplication, d *model.InvoiceDocument) {
			d.Status = model.InvoiceDocumentStatusMissing
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "superseded", userID: 11, mutate: func(_ *model.InvoiceApplication, d *model.InvoiceDocument) {
			d.Status = model.InvoiceDocumentStatusSuperseded
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "deleting", userID: 11, mutate: func(_ *model.InvoiceApplication, d *model.InvoiceDocument) {
			d.Status = model.InvoiceDocumentStatusDeleting
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "deleted", userID: 11, mutate: func(_ *model.InvoiceApplication, d *model.InvoiceDocument) {
			d.Status = model.InvoiceDocumentStatusDeleted
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "missing key", userID: 11, mutate: func(_ *model.InvoiceApplication, d *model.InvoiceDocument) {
			d.ObjectKey = nil
		}, want: ErrInvoiceDocumentUnavailable},
		{name: "expired", userID: 11, mutate: func(_ *model.InvoiceApplication, d *model.InvoiceDocument) {
			expired := int64(1_005)
			d.ExpiresAt = &expired
		}, want: ErrInvoiceDocumentUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application, document := downloadableInvoiceFixture()
			if test.mutate != nil {
				test.mutate(&application, document)
			}
			store := setupInvoiceDownloadTest(t, &application, document)
			_, err := GetInvoiceDocumentDownload(context.Background(), test.userID, application.ID)
			assert.ErrorIs(t, err, test.want)
			assert.Zero(t, store.headCalls)
			assert.Zero(t, store.presignCalls)
		})
	}
}

func TestGetInvoiceDocumentDownloadBoundsPresignTTL(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	store := setupInvoiceDownloadTest(t, &application, document)

	url, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.NoError(t, err)
	assert.Equal(t, store.url, url)
	assert.Equal(t, 5*time.Minute, store.ttl)
	assert.Equal(t, []string{"head", "presign"}, store.operationLog)

	nearExpiry := int64(1_040)
	require.NoError(t, model.DB.Model(&model.InvoiceDocument{}).Where("id = ?", *application.ActiveDocumentID).Update("expires_at", nearExpiry).Error)
	_, err = GetInvoiceDocumentDownload(context.Background(), 11, application.ID)
	require.NoError(t, err)
	assert.Equal(t, 35*time.Second, store.ttl)
}

func TestGetInvoiceDocumentDownloadExpirySkewBoundary(t *testing.T) {
	for _, test := range []struct {
		name      string
		expiresAt int64
		wantTTL   time.Duration
		wantErr   error
	}{
		{name: "six seconds remain", expiresAt: 1_006, wantTTL: time.Second},
		{name: "five seconds remain", expiresAt: 1_005, wantErr: ErrInvoiceDocumentUnavailable},
		{name: "four seconds remain", expiresAt: 1_004, wantErr: ErrInvoiceDocumentUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			application, document := downloadableInvoiceFixture()
			document.ExpiresAt = &test.expiresAt
			store := setupInvoiceDownloadTest(t, &application, document)
			_, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				assert.Zero(t, store.headCalls)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantTTL, store.ttl)
		})
	}
}

func TestGetInvoiceDocumentDownloadReconcilesMissingAndIntegrityMismatch(t *testing.T) {
	tests := []struct {
		name         string
		mutateStore  func(*invoiceDownloadStoreStub)
		wantCategory *string
	}{
		{name: "object absent", mutateStore: func(store *invoiceDownloadStoreStub) { store.headErr = ErrInvoiceObjectNotFound }},
		{name: "size mismatch", mutateStore: func(store *invoiceDownloadStoreStub) { store.head.SizeBytes++ }, wantCategory: downloadStringPointer(model.InvoiceDocumentDeleteErrorObjectIntegrityMismatch)},
		{name: "checksum mismatch", mutateStore: func(store *invoiceDownloadStoreStub) { store.head.ChecksumSHA256 = "different" }, wantCategory: downloadStringPointer(model.InvoiceDocumentDeleteErrorObjectIntegrityMismatch)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application, document := downloadableInvoiceFixture()
			store := setupInvoiceDownloadTest(t, &application, document)
			test.mutateStore(store)

			_, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

			require.ErrorIs(t, err, ErrInvoiceDocumentUnavailable)
			assert.Equal(t, 1, store.headCalls)
			assert.Zero(t, store.presignCalls)
			var persisted model.InvoiceDocument
			require.NoError(t, model.DB.First(&persisted, *application.ActiveDocumentID).Error)
			assert.Equal(t, model.InvoiceDocumentStatusMissing, persisted.Status)
			assert.Equal(t, test.wantCategory, persisted.DeleteErrorCategory)
		})
	}
}

func TestGetInvoiceDocumentDownloadInfrastructureFailuresPreserveState(t *testing.T) {
	tests := []struct {
		name        string
		mutateDoc   func(*model.InvoiceDocument)
		mutateStore func(*invoiceDownloadStoreStub)
	}{
		{name: "malformed checksum", mutateDoc: func(document *model.InvoiceDocument) { document.SHA256 = "not-hex" }},
		{name: "no such bucket", mutateStore: func(store *invoiceDownloadStoreStub) { store.headErr = ErrInvoiceObjectBucketUnavailable }},
		{name: "ambiguous terminal head", mutateStore: func(store *invoiceDownloadStoreStub) { store.headErr = ErrInvoiceObjectTerminal }},
		{name: "retryable head", mutateStore: func(store *invoiceDownloadStoreStub) { store.headErr = ErrInvoiceObjectRetryable }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application, document := downloadableInvoiceFixture()
			if test.mutateDoc != nil {
				test.mutateDoc(document)
			}
			store := setupInvoiceDownloadTest(t, &application, document)
			if test.mutateStore != nil {
				test.mutateStore(store)
			}

			_, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrInvoiceDocumentUnavailable)
			assert.Zero(t, store.presignCalls)
			var persisted model.InvoiceDocument
			require.NoError(t, model.DB.First(&persisted, *application.ActiveDocumentID).Error)
			assert.Equal(t, model.InvoiceDocumentStatusAvailable, persisted.Status)
			assert.Nil(t, persisted.DeleteErrorCategory)
		})
	}
}

func TestGetInvoiceDocumentDownloadCASLossDoesNotPresign(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	store := setupInvoiceDownloadTest(t, &application, document)
	store.headErr = ErrInvoiceObjectNotFound
	store.beforeHead = func() {
		require.NoError(t, model.DB.Model(&model.InvoiceDocument{}).
			Where("id = ?", *application.ActiveDocumentID).Update("operation_token", "replacement-token").Error)
	}

	_, err := GetInvoiceDocumentDownload(context.Background(), 11, application.ID)

	require.ErrorIs(t, err, ErrInvoiceDocumentUnavailable)
	assert.Zero(t, store.presignCalls)
	var persisted model.InvoiceDocument
	require.NoError(t, model.DB.First(&persisted, *application.ActiveDocumentID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, persisted.Status)
	assert.Equal(t, "replacement-token", persisted.OperationToken)
}

func downloadStringPointer(value string) *string { return &value }

func TestInvoiceSummaryUsesDownloadEligibility(t *testing.T) {
	application, document := downloadableInvoiceFixture()
	document.ID = 41
	document.ApplicationID = 9
	application.ID = 9
	application.ActiveDocumentID = &document.ID

	assert.True(t, invoiceApplicationSummaryAt(&application, document, time.Unix(1_000, 0)).CanDownload)
	application.PaymentReviewStatus = constant.InvoicePaymentReviewStatusResolvedVoided
	assert.False(t, invoiceApplicationSummaryAt(&application, document, time.Unix(1_000, 0)).CanDownload)
}
