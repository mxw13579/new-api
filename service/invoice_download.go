package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
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
	expectedChecksum, err := invoiceObjectChecksum(document.SHA256)
	if err != nil {
		return nil, ErrInvoiceObjectTerminal
	}
	object, err := store.Get(ctx, *document.ObjectKey, *document.ObjectETag)
	if errors.Is(err, ErrInvoiceObjectNotFound) {
		return nil, markInvoiceDocumentMissing(document, nil, now.Unix())
	}
	if err != nil {
		if errors.Is(err, ErrInvoiceObjectIntegrityUnavailable) {
			return nil, fmt.Errorf("%w: object integrity unavailable", ErrInvoiceDocumentUnavailable)
		}
		return nil, err
	}
	if object.Body == nil {
		return nil, fmt.Errorf("%w: object body unavailable", ErrInvoiceDocumentUnavailable)
	}
	content, readErr := io.ReadAll(io.LimitReader(object.Body, InvoicePDFMaxBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("%w: object read interrupted", ErrInvoiceDocumentRetryable)
	}
	if int64(len(content)) > InvoicePDFMaxBytes || int64(len(content)) != document.SizeBytes || object.SizeBytes != document.SizeBytes ||
		object.ChecksumSHA256 != expectedChecksum || object.ETag != *document.ObjectETag {
		return nil, fmt.Errorf("%w: object integrity unavailable", ErrInvoiceDocumentUnavailable)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != document.SHA256 {
		return nil, fmt.Errorf("%w: object integrity unavailable", ErrInvoiceDocumentUnavailable)
	}
	if closeErr != nil {
		logger.LogWarn(ctx, "invoice document body close failed after verified read")
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
