package service

import (
	"context"
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
	url string
	ttl time.Duration
	err error
}

func (s *invoiceDownloadStoreStub) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}
func (s *invoiceDownloadStoreStub) Copy(context.Context, string, string) error { return nil }
func (s *invoiceDownloadStoreStub) Head(context.Context, string) (InvoiceObjectHead, error) {
	return InvoiceObjectHead{}, nil
}
func (s *invoiceDownloadStoreStub) Delete(context.Context, string) error { return nil }
func (s *invoiceDownloadStoreStub) PresignGet(_ context.Context, _ string, ttl time.Duration) (string, error) {
	s.ttl = ttl
	return s.url, s.err
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
	store := &invoiceDownloadStoreStub{url: "https://signed.example.test/private-token"}
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
	application := model.InvoiceApplication{
		ApplicationNo: "INV-DOWNLOAD", UserID: 11, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusIssued,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	document := &model.InvoiceDocument{
		ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusAvailable,
		ObjectKey: &objectKey, ExpiresAt: &expiresAt, OperationToken: "token", UploadedBy: 1,
	}
	return application, document
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
			setupInvoiceDownloadTest(t, &application, document)
			_, err := GetInvoiceDocumentDownload(context.Background(), test.userID, application.ID)
			assert.ErrorIs(t, err, test.want)
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

	nearExpiry := int64(1_040)
	require.NoError(t, model.DB.Model(&model.InvoiceDocument{}).Where("id = ?", *application.ActiveDocumentID).Update("expires_at", nearExpiry).Error)
	_, err = GetInvoiceDocumentDownload(context.Background(), 11, application.ID)
	require.NoError(t, err)
	assert.Equal(t, 35*time.Second, store.ttl)
}

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
