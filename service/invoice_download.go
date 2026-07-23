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
	// ErrInvoiceDocumentUnavailable indicates that lifecycle or retention state currently forbids a download URL.
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

// GetInvoiceDocumentDownload returns a short-lived signed URL only for the owner's active downloadable document.
func GetInvoiceDocumentDownload(ctx context.Context, userID int, applicationID int64) (string, error) {
	application, err := model.GetInvoiceApplication(applicationID, &userID)
	if err != nil {
		return "", err
	}
	document, err := model.GetInvoiceDocument(application.ActiveDocumentID)
	if err != nil {
		return "", err
	}
	now := invoiceDownloadNow()
	ttl, err := invoiceDocumentDownloadTTL(application, document, now)
	if err != nil {
		return "", err
	}
	expectedChecksum, err := invoiceObjectChecksum(document.SHA256)
	if err != nil {
		return "", ErrInvoiceObjectTerminal
	}
	store, err := newInvoiceDownloadStore()
	if err != nil {
		return "", err
	}
	if !invoiceObjectStoreMatchesBucket(store, document.R2Bucket) {
		return "", ErrInvoiceObjectTerminal
	}
	head, err := store.Head(ctx, *document.ObjectKey)
	if errors.Is(err, ErrInvoiceObjectNotFound) {
		return "", markInvoiceDocumentMissing(document, nil, now.Unix())
	}
	if err != nil {
		return "", err
	}
	if head.SizeBytes != document.SizeBytes || head.ChecksumSHA256 != expectedChecksum {
		category := model.InvoiceDocumentDeleteErrorObjectIntegrityMismatch
		return "", markInvoiceDocumentMissing(document, &category, now.Unix())
	}
	return store.PresignGet(ctx, *document.ObjectKey, ttl)
}

func markInvoiceDocumentMissing(document *model.InvoiceDocument, category *string, now int64) error {
	updated := model.DB.Model(&model.InvoiceDocument{}).
		Where("id = ? AND status = ? AND operation_token = ?", document.ID, model.InvoiceDocumentStatusAvailable, document.OperationToken).
		Updates(map[string]any{
			"status": model.InvoiceDocumentStatusMissing, "delete_error_category": category, "updated_at": now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		if _, err := model.GetInvoiceDocument(&document.ID); err != nil {
			return err
		}
	}
	return ErrInvoiceDocumentUnavailable
}
