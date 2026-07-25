package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openInvoiceCrosslaneTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&InvoiceApplication{}, &InvoiceIssuance{}, &InvoiceDocument{}))
	return db
}

func TestInvoiceDocumentApplicationContractPreservesIssuanceInfrastructureError(t *testing.T) {
	db := openInvoiceCrosslaneTestDB(t)
	now := time.Now().Unix()
	application := InvoiceApplication{
		ApplicationNo: "INV-CROSSLANE-ERROR", UserID: 43, RequestID: "request-error", RequestFingerprint: "fingerprint-error",
		Type: constant.InvoiceTypeCompany, Status: constant.InvoiceApplicationStatusApproved,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		AmountMinor: 1234, FeeStatus: constant.InvoiceFeeStatusNotRequired,
		ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: now,
	}
	require.NoError(t, db.Create(&application).Error)
	document := InvoiceDocument{
		ApplicationID: application.ID, R2Bucket: "private", ContentType: InvoicePDFContentType,
		SizeBytes: 32, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Status: InvoiceDocumentStatusValidating, OperationToken: "token-error", OperationStartedAt: now,
		UploadedBy: 7, UploadedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&document).Error)
	sentinel := errors.New("issuance database unavailable")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:invoice-issuance-infrastructure-error", func(tx *gorm.DB) {
		if tx.Statement.Table == "invoice_issuances" {
			tx.AddError(sentinel)
		}
	}))

	err := db.Transaction(func(tx *gorm.DB) error {
		_, finalizeErr := NewInvoiceDocumentApplicationContract().FinalizeDocumentTx(tx, FinalizeInvoiceDocumentRequest{
			ApplicationID: application.ID, DocumentID: document.ID, OperationToken: document.OperationToken,
			ExpectedStatus:              constant.InvoiceApplicationStatusApproved,
			ExpectedPaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
			Issuance: InvoiceIssuanceFacts{InvoiceNumber: "NO-ERROR", InvoiceDate: now,
				FaceAmountMinor: application.AmountMinor, Currency: application.Currency},
			PDFFactsAttested: true, AttestedBy: 7,
		})
		return finalizeErr
	})
	require.ErrorIs(t, err, sentinel)
	assert.NotErrorIs(t, err, ErrInvoiceIssuanceConflict)
}

func TestInvoiceDocumentApplicationContractFinalizesAtomically(t *testing.T) {
	db := openInvoiceCrosslaneTestDB(t)
	now := time.Now().Unix()
	application := InvoiceApplication{
		ApplicationNo: "INV-CROSSLANE-1", UserID: 41, RequestID: "request-1", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypeCompany, Status: constant.InvoiceApplicationStatusApproved,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		AmountMinor: 1234, FeeStatus: constant.InvoiceFeeStatusNotRequired,
		ProfileSnapshot: `{"type":"company","title":"Example","tax_number":"TAX","version":1}`,
		PolicySnapshot:  `{}`, SubmittedAt: now,
	}
	require.NoError(t, db.Create(&application).Error)
	document := InvoiceDocument{
		ApplicationID: application.ID, R2Bucket: "private", ContentType: InvoicePDFContentType,
		SizeBytes: 32, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status: InvoiceDocumentStatusValidating, OperationToken: "token", OperationStartedAt: now,
		UploadedBy: 7, UploadedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&document).Error)
	contract := NewInvoiceDocumentApplicationContract()

	err := db.Transaction(func(tx *gorm.DB) error {
		prepared, err := contract.PrepareDocumentTx(tx, PrepareInvoiceDocumentRequest{
			ApplicationID: application.ID, ExpectedStatus: constant.InvoiceApplicationStatusApproved,
			ExpectedPaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		})
		require.NoError(t, err)
		assert.Equal(t, application.UserID, prepared.UserID)
		assert.Equal(t, application.ProfileSnapshot, prepared.ProfileSnapshot)

		finalized, err := contract.FinalizeDocumentTx(tx, FinalizeInvoiceDocumentRequest{
			ApplicationID: application.ID, DocumentID: document.ID, OperationToken: document.OperationToken,
			ExpectedStatus:              constant.InvoiceApplicationStatusApproved,
			ExpectedPaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
			Issuance:                    InvoiceIssuanceFacts{InvoiceNumber: "NO-1", InvoiceCode: "CODE", InvoiceDate: now, FaceAmountMinor: 1234, Currency: constant.InvoiceCurrencyCNY},
			PDFFactsAttested:            true, AttestedBy: 7,
		})
		require.NoError(t, err)
		assert.Equal(t, document.ID, finalized.ActiveDocumentID)
		assert.Equal(t, constant.InvoiceApplicationStatusIssued, finalized.ApplicationStatus)
		return nil
	})
	require.NoError(t, err)

	var persisted InvoiceApplication
	require.NoError(t, db.First(&persisted, application.ID).Error)
	assert.Equal(t, constant.InvoiceApplicationStatusIssued, persisted.Status)
	require.NotNil(t, persisted.ActiveDocumentID)
	assert.Equal(t, document.ID, *persisted.ActiveDocumentID)
	require.NotNil(t, persisted.IssuedAt)
	var issuance InvoiceIssuance
	require.NoError(t, db.Where("application_id = ?", application.ID).First(&issuance).Error)
	assert.Equal(t, "NO-1", issuance.InvoiceNumber)
}

func TestInvoiceDocumentApplicationContractRejectsStaleCAS(t *testing.T) {
	db := openInvoiceCrosslaneTestDB(t)
	application := InvoiceApplication{
		ApplicationNo: "INV-CROSSLANE-2", UserID: 42, RequestID: "request-2", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusReviewing,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		AmountMinor: 100, FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, db.Create(&application).Error)
	contract := NewInvoiceDocumentApplicationContract()

	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := contract.PrepareDocumentTx(tx, PrepareInvoiceDocumentRequest{
			ApplicationID: application.ID, ExpectedStatus: constant.InvoiceApplicationStatusApproved,
			ExpectedPaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		})
		return err
	})
	assert.ErrorIs(t, err, ErrInvoiceStateConflict)
}
