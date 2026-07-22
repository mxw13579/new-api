package model

import (
	"errors"

	"gorm.io/gorm"
)

var (
	ErrInvoiceNotFound              = errors.New("invoice not found")
	ErrInvoiceStateConflict         = errors.New("invoice state conflict")
	ErrInvoicePaymentReviewConflict = errors.New("invoice payment review conflict")
	ErrInvoiceDocumentConflict      = errors.New("invoice document conflict")
	ErrInvoiceIssuanceConflict      = errors.New("invoice issuance conflict")

	ErrInvoicePaymentSourceInvalidRequest   = errors.New("invoice payment source invalid request")
	ErrInvoicePaymentSourceNotEligible      = errors.New("invoice payment source not eligible")
	ErrInvoicePaymentSourceClaimConflict    = errors.New("invoice payment source claim conflict")
	ErrInvoicePaymentSourceEvidenceConflict = errors.New("invoice payment source evidence conflict")
)

type TopUpVersionExpectation struct {
	TopUpID                int
	ExpectedPaymentVersion int64
}

type ClaimInvoiceTopUpsRequest struct {
	UserID        int
	ApplicationID int64
	TopUps        []TopUpVersionExpectation
}

type ClaimedTopUpEvidence struct {
	TopUpID         int
	MerchantTradeNo string
	PaidAmountMinor int64
	Currency        string
	ProductSnapshot string
	PaidAt          int64
	EvidenceSource  string
	PaymentVersion  int64
}

type ReleaseInvoiceTopUpsRequest struct {
	UserID        int
	ApplicationID int64
	TopUps        []TopUpVersionExpectation
}

type ReleasedTopUpVersion struct {
	TopUpID        int
	PaymentVersion int64
}

type InvoicePaymentSource interface {
	ClaimTopUpsTx(*gorm.DB, ClaimInvoiceTopUpsRequest) ([]ClaimedTopUpEvidence, error)
	ReleaseTopUpsTx(*gorm.DB, ReleaseInvoiceTopUpsRequest) ([]ReleasedTopUpVersion, error)
}

type InvoiceDocumentApplicationContract interface {
	PrepareDocumentTx(*gorm.DB, PrepareInvoiceDocumentRequest) (PrepareInvoiceDocumentResult, error)
	FinalizeDocumentTx(*gorm.DB, FinalizeInvoiceDocumentRequest) (FinalizeInvoiceDocumentResult, error)
	ReplaceDocumentTx(*gorm.DB, ReplaceInvoiceDocumentRequest) (ReplaceInvoiceDocumentResult, error)
	RevokeDocumentTx(*gorm.DB, RevokeInvoiceDocumentRequest) error
}

type InvoiceIssuanceFacts struct {
	InvoiceNumber   string
	InvoiceCode     string
	InvoiceDate     int64
	FaceAmountMinor int64
	Currency        string
}

type PrepareInvoiceDocumentRequest struct {
	ApplicationID               int64
	ExpectedStatus              string
	ExpectedPaymentReviewStatus string
}

type PrepareInvoiceDocumentResult struct {
	UserID                   int
	ApplicationNo            string
	ProfileSnapshot          string
	AmountMinor              int64
	Currency                 string
	ExpectedActiveDocumentID *int64
}

type FinalizeInvoiceDocumentRequest struct {
	ApplicationID               int64
	DocumentID                  int64
	OperationToken              string
	ExpectedStatus              string
	ExpectedPaymentReviewStatus string
	ExpectedActiveDocumentID    *int64
	Issuance                    InvoiceIssuanceFacts
	PDFFactsAttested            bool
	AttestedBy                  int
}

type FinalizeInvoiceDocumentResult struct {
	IssuanceID        int64
	ActiveDocumentID  int64
	ApplicationStatus string
}

type ReplaceInvoiceDocumentRequest struct {
	ApplicationID               int64
	NewDocumentID               int64
	OperationToken              string
	ExpectedStatus              string
	ExpectedPaymentReviewStatus string
	ExpectedActiveDocumentID    int64
	ExpectedIssuanceID          int64
	PDFFactsAttested            bool
	AttestedBy                  int
}

type ReplaceInvoiceDocumentResult struct {
	SupersededDocumentID int64
	ActiveDocumentID     int64
}

type RevokeInvoiceDocumentRequest struct {
	ApplicationID               int64
	ExpectedActiveDocumentID    int64
	ExpectedPaymentReviewStatus string
	Reason                      string
}
