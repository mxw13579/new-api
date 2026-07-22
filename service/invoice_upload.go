package service

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

func UploadInvoiceDocument(ctx context.Context, actorID int, applicationID int64, request dto.InvoiceDocumentUploadRequest, reader io.Reader) error {
	if actorID <= 0 || applicationID <= 0 || reader == nil || !validInvoiceDocumentUploadRequest(request) {
		return model.ErrInvoiceDocumentConflict
	}
	store, err := NewInvoiceR2StoreFromEnvironment()
	if err != nil {
		return err
	}
	return uploadInvoiceDocument(ctx, model.DB, store, store.Bucket(), operation_setting.GetInvoiceSetting().PDFRetentionDays,
		actorID, applicationID, request, reader, time.Now().Unix())
}

func uploadInvoiceDocument(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, bucket string, retentionDays, actorID int,
	applicationID int64, request dto.InvoiceDocumentUploadRequest, reader io.Reader, now int64,
) error {
	if db == nil || store == nil || bucket == "" || retentionDays <= 0 || now <= 0 || actorID <= 0 || applicationID <= 0 || reader == nil ||
		!validInvoiceDocumentUploadRequest(request) {
		return model.ErrInvoiceDocumentConflict
	}
	var application model.InvoiceApplication
	if err := db.First(&application, applicationID).Error; err != nil {
		return err
	}
	paymentReviewPermitted := application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusNone ||
		application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusResolvedValid
	initial := request.ExpectedStatus == constant.InvoiceApplicationStatusApproved && application.ActiveDocumentID == nil
	replacement := request.ExpectedStatus == constant.InvoiceApplicationStatusIssued && application.ActiveDocumentID != nil
	if application.Status != request.ExpectedStatus || (!initial && !replacement) || !paymentReviewPermitted {
		return model.ErrInvoiceStateConflict
	}
	var previous model.InvoiceDocument
	if replacement {
		if err := db.Where("id = ? AND application_id = ? AND status = ?", *application.ActiveDocumentID, applicationID,
			model.InvoiceDocumentStatusAvailable).First(&previous).Error; err != nil {
			return err
		}
		if previous.IssuanceID == nil {
			return model.ErrInvoiceIssuanceConflict
		}
		var issuance model.InvoiceIssuance
		if err := db.Where("id = ? AND application_id = ?", *previous.IssuanceID, applicationID).First(&issuance).Error; err != nil {
			return err
		}
		if issuance.InvoiceNumber != strings.TrimSpace(request.InvoiceNumber) || issuance.InvoiceCode != strings.TrimSpace(request.InvoiceCode) ||
			issuance.InvoiceDate != request.InvoiceDate || issuance.FaceAmountMinor != request.FaceAmountMinor || issuance.Currency != request.Currency {
			return model.ErrInvoiceIssuanceConflict
		}
	}
	document, err := CreateInvoiceDocumentUpload(db, bucket, applicationID, actorID, now)
	if err != nil {
		return err
	}
	document, err = PromoteInvoiceDocument(ctx, db, store, document.ID, document.OperationToken, reader, now)
	if err != nil {
		return err
	}
	lifecycle := NewInvoiceDocumentLifecycle(db, store, model.NewInvoiceDocumentApplicationContract(), retentionDays)
	if initial {
		_, err = lifecycle.Finalize(ctx, FinalizeInvoiceDocumentOperation{
			ApplicationID: applicationID, DocumentID: document.ID, OperationToken: document.OperationToken,
			ExpectedStatus: request.ExpectedStatus, ExpectedPaymentReviewStatus: application.PaymentReviewStatus,
			ExpectedActiveDocumentID: application.ActiveDocumentID,
			Issuance: model.InvoiceIssuanceFacts{
				InvoiceNumber: request.InvoiceNumber, InvoiceCode: request.InvoiceCode, InvoiceDate: request.InvoiceDate,
				FaceAmountMinor: request.FaceAmountMinor, Currency: request.Currency,
			},
			PDFFactsAttested: true, AttestedBy: actorID, Now: now,
		})
		return err
	}
	_, err = lifecycle.Replace(ctx, ReplaceInvoiceDocumentOperation{
		ApplicationID: applicationID, NewDocumentID: document.ID, OperationToken: document.OperationToken,
		ExpectedStatus: request.ExpectedStatus, ExpectedPaymentReviewStatus: application.PaymentReviewStatus,
		ExpectedActiveDocumentID: *application.ActiveDocumentID, ExpectedIssuanceID: *previous.IssuanceID,
		PDFFactsAttested: true, AttestedBy: actorID, Now: now,
	})
	return err
}

func validInvoiceDocumentUploadRequest(request dto.InvoiceDocumentUploadRequest) bool {
	return (request.ExpectedStatus == constant.InvoiceApplicationStatusApproved || request.ExpectedStatus == constant.InvoiceApplicationStatusIssued) &&
		strings.TrimSpace(request.InvoiceNumber) != "" && request.InvoiceDate > 0 && request.FaceAmountMinor > 0 &&
		request.Currency == constant.InvoiceCurrencyCNY && request.PDFFactsAttested
}
