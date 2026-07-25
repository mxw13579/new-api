package model

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

// InvoiceDocumentApplication implements document lifecycle compare-and-swap operations against invoice applications.
type InvoiceDocumentApplication struct{}

var _ InvoiceDocumentApplicationContract = (*InvoiceDocumentApplication)(nil)

// NewInvoiceDocumentApplicationContract returns the aggregate adapter used by the document lifecycle service.
func NewInvoiceDocumentApplicationContract() InvoiceDocumentApplicationContract {
	return &InvoiceDocumentApplication{}
}

// PrepareDocumentTx locks an application and returns the facts required to validate a document transition.
func (applicationContract *InvoiceDocumentApplication) PrepareDocumentTx(tx *gorm.DB, request PrepareInvoiceDocumentRequest) (PrepareInvoiceDocumentResult, error) {
	if applicationContract == nil || tx == nil || request.ApplicationID <= 0 || request.ExpectedStatus == "" || request.ExpectedPaymentReviewStatus == "" {
		return PrepareInvoiceDocumentResult{}, ErrInvoiceDocumentConflict
	}
	var application InvoiceApplication
	err := lockForUpdate(tx).First(&application, request.ApplicationID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PrepareInvoiceDocumentResult{}, ErrInvoiceNotFound
	}
	if err != nil {
		return PrepareInvoiceDocumentResult{}, err
	}
	paymentReviewPermitted := application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusNone ||
		application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusResolvedValid
	if application.Status != request.ExpectedStatus || application.PaymentReviewStatus != request.ExpectedPaymentReviewStatus || !paymentReviewPermitted {
		return PrepareInvoiceDocumentResult{}, ErrInvoiceStateConflict
	}
	return PrepareInvoiceDocumentResult{
		UserID: application.UserID, ApplicationNo: application.ApplicationNo,
		ProfileSnapshot: application.ProfileSnapshot, AmountMinor: application.AmountMinor,
		Currency: application.Currency, ExpectedActiveDocumentID: application.ActiveDocumentID,
	}, nil
}

// FinalizeDocumentTx creates immutable issuance facts and marks a validated document active in one transaction.
func (applicationContract *InvoiceDocumentApplication) FinalizeDocumentTx(tx *gorm.DB, request FinalizeInvoiceDocumentRequest) (FinalizeInvoiceDocumentResult, error) {
	if applicationContract == nil || tx == nil || request.ApplicationID <= 0 || request.DocumentID <= 0 || request.OperationToken == "" ||
		request.ExpectedStatus != constant.InvoiceApplicationStatusApproved || request.AttestedBy <= 0 || !request.PDFFactsAttested ||
		strings.TrimSpace(request.Issuance.InvoiceNumber) == "" || request.Issuance.InvoiceDate <= 0 || request.Issuance.FaceAmountMinor <= 0 ||
		request.Issuance.Currency != constant.InvoiceCurrencyCNY {
		return FinalizeInvoiceDocumentResult{}, ErrInvoiceDocumentConflict
	}
	prepared, err := applicationContract.PrepareDocumentTx(tx, PrepareInvoiceDocumentRequest{
		ApplicationID: request.ApplicationID, ExpectedStatus: request.ExpectedStatus,
		ExpectedPaymentReviewStatus: request.ExpectedPaymentReviewStatus,
	})
	if err != nil {
		return FinalizeInvoiceDocumentResult{}, err
	}
	if prepared.AmountMinor != request.Issuance.FaceAmountMinor || prepared.Currency != request.Issuance.Currency {
		return FinalizeInvoiceDocumentResult{}, ErrInvoiceIssuanceConflict
	}
	if prepared.ExpectedActiveDocumentID != nil || request.ExpectedActiveDocumentID != nil {
		return FinalizeInvoiceDocumentResult{}, ErrInvoiceStateConflict
	}
	var document InvoiceDocument
	err = lockForUpdate(tx).Where("id = ? AND application_id = ? AND operation_token = ? AND status = ?", request.DocumentID,
		request.ApplicationID, request.OperationToken, InvoiceDocumentStatusValidating).First(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FinalizeInvoiceDocumentResult{}, ErrInvoiceDocumentConflict
	}
	if err != nil {
		return FinalizeInvoiceDocumentResult{}, err
	}
	now := time.Now().Unix()
	issuance := InvoiceIssuance{
		ApplicationID: request.ApplicationID, InvoiceNumber: strings.TrimSpace(request.Issuance.InvoiceNumber),
		InvoiceCode: strings.TrimSpace(request.Issuance.InvoiceCode), InvoiceDate: request.Issuance.InvoiceDate,
		FaceAmountMinor: request.Issuance.FaceAmountMinor, Currency: request.Issuance.Currency,
		CreatedBy: request.AttestedBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&issuance).Error; err != nil {
		if isInvoiceIssuanceUniquenessConflict(err) {
			return FinalizeInvoiceDocumentResult{}, ErrInvoiceIssuanceConflict
		}
		return FinalizeInvoiceDocumentResult{}, err
	}
	updated := tx.Model(&InvoiceApplication{}).
		Where("id = ? AND status = ? AND payment_review_status = ? AND active_document_id IS NULL", request.ApplicationID,
			request.ExpectedStatus, request.ExpectedPaymentReviewStatus).
		Updates(map[string]any{
			"status": constant.InvoiceApplicationStatusIssued, "active_document_id": request.DocumentID,
			"issued_at": now, "updated_at": now,
		})
	if updated.Error != nil {
		return FinalizeInvoiceDocumentResult{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return FinalizeInvoiceDocumentResult{}, ErrInvoiceStateConflict
	}
	return FinalizeInvoiceDocumentResult{
		IssuanceID: issuance.ID, ActiveDocumentID: request.DocumentID,
		ApplicationStatus: constant.InvoiceApplicationStatusIssued,
	}, nil
}

// ReplaceDocumentTx switches an issued application to a validated replacement document using compare-and-swap guards.
func (applicationContract *InvoiceDocumentApplication) ReplaceDocumentTx(tx *gorm.DB, request ReplaceInvoiceDocumentRequest) (ReplaceInvoiceDocumentResult, error) {
	if applicationContract == nil || tx == nil || request.ApplicationID <= 0 || request.NewDocumentID <= 0 || request.OperationToken == "" ||
		request.ExpectedStatus != constant.InvoiceApplicationStatusIssued || request.ExpectedActiveDocumentID <= 0 || request.ExpectedIssuanceID <= 0 ||
		request.AttestedBy <= 0 || !request.PDFFactsAttested {
		return ReplaceInvoiceDocumentResult{}, ErrInvoiceDocumentConflict
	}
	prepared, err := applicationContract.PrepareDocumentTx(tx, PrepareInvoiceDocumentRequest{
		ApplicationID: request.ApplicationID, ExpectedStatus: request.ExpectedStatus,
		ExpectedPaymentReviewStatus: request.ExpectedPaymentReviewStatus,
	})
	if err != nil {
		return ReplaceInvoiceDocumentResult{}, err
	}
	if prepared.ExpectedActiveDocumentID == nil || *prepared.ExpectedActiveDocumentID != request.ExpectedActiveDocumentID {
		return ReplaceInvoiceDocumentResult{}, ErrInvoiceStateConflict
	}
	var issuance InvoiceIssuance
	if err := lockForUpdate(tx).Where("id = ? AND application_id = ?", request.ExpectedIssuanceID, request.ApplicationID).First(&issuance).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ReplaceInvoiceDocumentResult{}, ErrInvoiceIssuanceConflict
		}
		return ReplaceInvoiceDocumentResult{}, err
	}
	var document InvoiceDocument
	if err := lockForUpdate(tx).Where("id = ? AND application_id = ? AND operation_token = ? AND status = ?", request.NewDocumentID,
		request.ApplicationID, request.OperationToken, InvoiceDocumentStatusValidating).First(&document).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ReplaceInvoiceDocumentResult{}, ErrInvoiceDocumentConflict
		}
		return ReplaceInvoiceDocumentResult{}, err
	}
	now := time.Now().Unix()
	updated := tx.Model(&InvoiceApplication{}).
		Where("id = ? AND status = ? AND payment_review_status = ? AND active_document_id = ?", request.ApplicationID,
			request.ExpectedStatus, request.ExpectedPaymentReviewStatus, request.ExpectedActiveDocumentID).
		Updates(map[string]any{"active_document_id": request.NewDocumentID, "updated_at": now})
	if updated.Error != nil {
		return ReplaceInvoiceDocumentResult{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return ReplaceInvoiceDocumentResult{}, ErrInvoiceStateConflict
	}
	return ReplaceInvoiceDocumentResult{SupersededDocumentID: request.ExpectedActiveDocumentID, ActiveDocumentID: request.NewDocumentID}, nil
}

// RevokeDocumentTx removes the active document link and places the issued application on payment-review hold.
func (applicationContract *InvoiceDocumentApplication) RevokeDocumentTx(tx *gorm.DB, request RevokeInvoiceDocumentRequest) error {
	if applicationContract == nil || tx == nil || request.ApplicationID <= 0 || request.ExpectedActiveDocumentID <= 0 ||
		request.ExpectedPaymentReviewStatus == "" || strings.TrimSpace(request.Reason) == "" {
		return ErrInvoiceDocumentConflict
	}
	updated := tx.Model(&InvoiceApplication{}).
		Where("id = ? AND status = ? AND payment_review_status = ? AND active_document_id = ?", request.ApplicationID,
			constant.InvoiceApplicationStatusIssued, request.ExpectedPaymentReviewStatus, request.ExpectedActiveDocumentID).
		Updates(map[string]any{
			"active_document_id": nil, "payment_review_status": constant.InvoicePaymentReviewStatusPostIssueHold,
			"updated_at": time.Now().Unix(),
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrInvoiceStateConflict
	}
	return nil
}
