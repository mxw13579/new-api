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
)

func UploadInvoiceDocument(ctx context.Context, actorID int, applicationID int64, request dto.InvoiceDocumentUploadRequest, reader io.Reader) error {
	if actorID <= 0 || applicationID <= 0 || reader == nil || request.ExpectedStatus != constant.InvoiceApplicationStatusApproved ||
		strings.TrimSpace(request.InvoiceNumber) == "" || request.InvoiceDate <= 0 || request.FaceAmountMinor <= 0 ||
		request.Currency != constant.InvoiceCurrencyCNY || !request.PDFFactsAttested {
		return model.ErrInvoiceDocumentConflict
	}
	application, err := model.GetInvoiceApplication(applicationID, nil)
	if err != nil {
		return err
	}
	paymentReviewPermitted := application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusNone ||
		application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusResolvedValid
	if application.Status != request.ExpectedStatus || application.ActiveDocumentID != nil || !paymentReviewPermitted {
		return model.ErrInvoiceStateConflict
	}
	store, err := NewInvoiceR2StoreFromEnvironment()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	document, err := CreateInvoiceDocumentUpload(model.DB, store.Bucket(), applicationID, actorID, now)
	if err != nil {
		return err
	}
	document, err = PromoteInvoiceDocument(ctx, model.DB, store, document.ID, document.OperationToken, reader, now)
	if err != nil {
		return err
	}
	lifecycle := NewInvoiceDocumentLifecycle(model.DB, store, model.NewInvoiceDocumentApplicationContract(), operation_setting.GetInvoiceSetting().PDFRetentionDays)
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
