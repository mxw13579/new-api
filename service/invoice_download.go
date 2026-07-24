package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

var (
	// ErrInvoiceDocumentUnavailable indicates that lifecycle, retention, or immutable object state forbids a download.
	ErrInvoiceDocumentUnavailable = errors.New("invoice document unavailable")
	invoiceDownloadNow            = time.Now
	newInvoiceDownloadStore       = func() (InvoiceObjectStore, error) { return NewInvoiceR2StoreFromEnvironment() }
)

func invoiceDocumentDownloadEligible(application *model.InvoiceApplication, document *model.InvoiceDocument, now time.Time) error {
	paymentReviewPermitted := application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusNone ||
		application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusResolvedValid
	if application.Status != constant.InvoiceApplicationStatusIssued || !paymentReviewPermitted ||
		application.ActiveDocumentID == nil || document == nil || document.ID != *application.ActiveDocumentID ||
		document.ApplicationID != application.ID || document.Status != model.InvoiceDocumentStatusAvailable ||
		document.ObjectKey == nil || document.ExpiresAt == nil || *document.ExpiresAt <= now.Unix() || document.DeletedAt != nil {
		return ErrInvoiceDocumentUnavailable
	}
	return nil
}

func invoiceDocumentDownloadTTL(application *model.InvoiceApplication, document *model.InvoiceDocument, now time.Time) (time.Duration, error) {
	if err := invoiceDocumentDownloadEligible(application, document, now); err != nil {
		return 0, err
	}
	return time.Unix(*document.ExpiresAt, 0).Sub(now), nil
}

// GetInvoiceDocumentDownload returns owner-scoped PDF bytes only after a conditional read verifies every immutable fact.
func GetInvoiceDocumentDownload(ctx context.Context, userID int, applicationID int64) ([]byte, error) {
	application, err := model.GetInvoiceApplication(applicationID, &userID)
	if err != nil {
		return nil, err
	}
	document, err := model.GetInvoiceDocument(application.ActiveDocumentID)
	if err != nil {
		return nil, err
	}
	now := invoiceDownloadNow()
	if err := invoiceDocumentDownloadEligible(application, document, now); err != nil {
		return nil, err
	}
	store, err := newInvoiceDownloadStore()
	if err != nil {
		return nil, err
	}
	if err := bindInvoiceDocumentStoreAuthority(ctx, model.DB, store, document); err != nil {
		return nil, fmt.Errorf("%w: object authority unavailable", ErrInvoiceDocumentUnavailable)
	}
	if document.ObjectETag == nil || strings.TrimSpace(*document.ObjectETag) == "" {
		return nil, fmt.Errorf("%w: object integrity unavailable", ErrInvoiceDocumentUnavailable)
	}
	content, err := readVerifiedInvoiceObject(ctx, store, *document.ObjectKey, *document.ObjectETag, document.SizeBytes, document.SHA256)
	if errors.Is(err, ErrInvoiceObjectNotFound) {
		return nil, markInvoiceDocumentMissing(document, nil, now.Unix())
	}
	if err != nil {
		if errors.Is(err, ErrInvoiceObjectIntegrityUnavailable) {
			return nil, fmt.Errorf("%w: object integrity unavailable", ErrInvoiceDocumentUnavailable)
		}
		return nil, err
	}
	return content, nil
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
