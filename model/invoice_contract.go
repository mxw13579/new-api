package model

import (
	"errors"

	"gorm.io/gorm"
)

var (
	// ErrInvoiceNotFound indicates that the requested invoice aggregate is absent or outside the caller's ownership scope.
	ErrInvoiceNotFound = errors.New("invoice not found")
	// ErrInvoiceStateConflict indicates that an invoice lifecycle compare-and-swap precondition no longer matches.
	ErrInvoiceStateConflict = errors.New("invoice state conflict")
	// ErrInvoicePaymentReviewConflict indicates that payment review state blocks the requested invoice transition.
	ErrInvoicePaymentReviewConflict = errors.New("invoice payment review conflict")
	// ErrInvoiceDocumentConflict indicates that document identity, state, or operation ownership is inconsistent.
	ErrInvoiceDocumentConflict = errors.New("invoice document conflict")
	// ErrInvoiceIssuanceConflict indicates that immutable issuance facts conflict with the invoice aggregate.
	ErrInvoiceIssuanceConflict = errors.New("invoice issuance conflict")

	// ErrInvoicePaymentSourceInvalidRequest indicates a malformed payment-evidence claim or release request.
	ErrInvoicePaymentSourceInvalidRequest = errors.New("invoice payment source invalid request")
	// ErrInvoicePaymentSourceNotEligible indicates that a top-up cannot participate in an invoice application.
	ErrInvoicePaymentSourceNotEligible = errors.New("invoice payment source not eligible")
	// ErrInvoicePaymentSourceClaimConflict indicates that payment evidence was claimed or versioned concurrently.
	ErrInvoicePaymentSourceClaimConflict = errors.New("invoice payment source claim conflict")
	// ErrInvoicePaymentSourceEvidenceConflict indicates that immutable payment facts no longer match trusted evidence.
	ErrInvoicePaymentSourceEvidenceConflict = errors.New("invoice payment source evidence conflict")
)

// TopUpVersionExpectation binds a top-up to the payment version required by an atomic claim or release.
type TopUpVersionExpectation struct {
	TopUpID                int
	ExpectedPaymentVersion int64
}

// ClaimInvoiceTopUpsRequest describes the owner, application, and versioned top-ups to reserve atomically.
type ClaimInvoiceTopUpsRequest struct {
	UserID        int
	ApplicationID int64
	TopUps        []TopUpVersionExpectation
}

// ClaimedTopUpEvidence captures the immutable billing facts returned after a successful top-up reservation.
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

// ReleaseInvoiceTopUpsRequest describes the versioned top-up reservations to release from an application.
type ReleaseInvoiceTopUpsRequest struct {
	UserID        int
	ApplicationID int64
	TopUps        []TopUpVersionExpectation
}

// ReleasedTopUpVersion reports the payment version produced by releasing a top-up reservation.
type ReleasedTopUpVersion struct {
	TopUpID        int
	PaymentVersion int64
}

// InvoicePaymentSource defines atomic reservation and release of invoice-eligible payment evidence.
type InvoicePaymentSource interface {
	// ClaimTopUpsTx reserves versioned payment evidence within the caller's transaction.
	ClaimTopUpsTx(*gorm.DB, ClaimInvoiceTopUpsRequest) ([]ClaimedTopUpEvidence, error)
	// ReleaseTopUpsTx releases versioned payment evidence within the caller's transaction.
	ReleaseTopUpsTx(*gorm.DB, ReleaseInvoiceTopUpsRequest) ([]ReleasedTopUpVersion, error)
}

// InvoiceDocumentApplicationContract coordinates document lifecycle changes with the invoice aggregate transaction.
type InvoiceDocumentApplicationContract interface {
	// PrepareDocumentTx locks and validates the application state required by a document operation.
	PrepareDocumentTx(*gorm.DB, PrepareInvoiceDocumentRequest) (PrepareInvoiceDocumentResult, error)
	// FinalizeDocumentTx atomically binds first issuance facts and an active document to an application.
	FinalizeDocumentTx(*gorm.DB, FinalizeInvoiceDocumentRequest) (FinalizeInvoiceDocumentResult, error)
	// ReplaceDocumentTx atomically switches an issued application to a replacement document.
	ReplaceDocumentTx(*gorm.DB, ReplaceInvoiceDocumentRequest) (ReplaceInvoiceDocumentResult, error)
	// RevokeDocumentTx atomically removes an active document after a payment-review conflict.
	RevokeDocumentTx(*gorm.DB, RevokeInvoiceDocumentRequest) error
}

// InvoiceIssuanceFacts contains the immutable fiscal facts attested for an issued invoice.
type InvoiceIssuanceFacts struct {
	InvoiceNumber   string
	InvoiceCode     string
	InvoiceDate     int64
	FaceAmountMinor int64
	Currency        string
}

// PrepareInvoiceDocumentRequest defines the application state required before document finalization or replacement.
type PrepareInvoiceDocumentRequest struct {
	ApplicationID               int64
	ExpectedStatus              string
	ExpectedPaymentReviewStatus string
}

// PrepareInvoiceDocumentResult returns the aggregate facts needed to validate and bind a document operation.
type PrepareInvoiceDocumentResult struct {
	UserID                   int
	ApplicationNo            string
	ProfileSnapshot          string
	AmountMinor              int64
	Currency                 string
	ExpectedActiveDocumentID *int64
}

// FinalizeInvoiceDocumentRequest binds a validated document and attested issuance facts to an approved application.
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

// FinalizeInvoiceDocumentResult identifies the issuance and active document created by finalization.
type FinalizeInvoiceDocumentResult struct {
	IssuanceID        int64
	ActiveDocumentID  int64
	ApplicationStatus string
}

// ReplaceInvoiceDocumentRequest defines the compare-and-swap facts for replacing an issued invoice document.
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

// ReplaceInvoiceDocumentResult identifies the superseded and newly active document versions.
type ReplaceInvoiceDocumentResult struct {
	SupersededDocumentID int64
	ActiveDocumentID     int64
}

// RevokeInvoiceDocumentRequest defines the active-document and payment-review state required for revocation.
type RevokeInvoiceDocumentRequest struct {
	ApplicationID               int64
	ExpectedActiveDocumentID    int64
	ExpectedPaymentReviewStatus string
	Reason                      string
}
