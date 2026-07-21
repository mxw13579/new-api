package model

import (
	"errors"

	"gorm.io/gorm"
)

var (
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
