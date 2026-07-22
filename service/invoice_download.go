package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

const (
	invoiceDownloadMaxTTL    = 5 * time.Minute
	invoiceDownloadClockSkew = 5 * time.Second
)

var (
	ErrInvoiceDocumentUnavailable = errors.New("invoice document unavailable")
	invoiceDownloadNow            = time.Now
	newInvoiceDownloadStore       = func() (InvoiceObjectStore, error) { return NewInvoiceR2StoreFromEnvironment() }
)

func invoiceDocumentDownloadTTL(application *model.InvoiceApplication, document *model.InvoiceDocument, now time.Time) (time.Duration, error) {
	paymentReviewPermitted := application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusNone ||
		application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusResolvedValid
	if application.Status != constant.InvoiceApplicationStatusIssued || !paymentReviewPermitted ||
		application.ActiveDocumentID == nil || document == nil || document.ID != *application.ActiveDocumentID ||
		document.ApplicationID != application.ID || document.Status != model.InvoiceDocumentStatusAvailable ||
		document.ObjectKey == nil || document.ExpiresAt == nil || document.DeletedAt != nil {
		return 0, ErrInvoiceDocumentUnavailable
	}
	remaining := time.Unix(*document.ExpiresAt, 0).Sub(now) - invoiceDownloadClockSkew
	if remaining <= 0 {
		return 0, ErrInvoiceDocumentUnavailable
	}
	if remaining > invoiceDownloadMaxTTL {
		remaining = invoiceDownloadMaxTTL
	}
	return remaining, nil
}

func GetInvoiceDocumentDownload(ctx context.Context, userID int, applicationID int64) (string, error) {
	application, err := model.GetInvoiceApplication(applicationID, &userID)
	if err != nil {
		return "", err
	}
	document, err := model.GetInvoiceDocument(application.ActiveDocumentID)
	if err != nil {
		return "", err
	}
	ttl, err := invoiceDocumentDownloadTTL(application, document, invoiceDownloadNow())
	if err != nil {
		return "", err
	}
	store, err := newInvoiceDownloadStore()
	if err != nil {
		return "", err
	}
	return store.PresignGet(ctx, *document.ObjectKey, ttl)
}
