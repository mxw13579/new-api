package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceDocumentContractStub struct{}

func (invoiceDocumentContractStub) PrepareDocumentTx(*gorm.DB, PrepareInvoiceDocumentRequest) (PrepareInvoiceDocumentResult, error) {
	return PrepareInvoiceDocumentResult{}, nil
}
func (invoiceDocumentContractStub) FinalizeDocumentTx(*gorm.DB, FinalizeInvoiceDocumentRequest) (FinalizeInvoiceDocumentResult, error) {
	return FinalizeInvoiceDocumentResult{}, nil
}
func (invoiceDocumentContractStub) ReplaceDocumentTx(*gorm.DB, ReplaceInvoiceDocumentRequest) (ReplaceInvoiceDocumentResult, error) {
	return ReplaceInvoiceDocumentResult{}, nil
}
func (invoiceDocumentContractStub) RevokeDocumentTx(*gorm.DB, RevokeInvoiceDocumentRequest) error {
	return nil
}

func TestInvoiceDocumentApplicationContractIsFrozen(t *testing.T) {
	var contract InvoiceDocumentApplicationContract = invoiceDocumentContractStub{}
	assert.NotNil(t, contract)

	request := FinalizeInvoiceDocumentRequest{
		ApplicationID: 1, DocumentID: 2, OperationToken: "token",
		ExpectedStatus: "approved", ExpectedPaymentReviewStatus: "none",
		ExpectedActiveDocumentID: nil,
		Issuance:                 InvoiceIssuanceFacts{InvoiceNumber: "N", InvoiceCode: "C", InvoiceDate: 3, FaceAmountMinor: 4, Currency: "CNY"},
		PDFFactsAttested:         true, AttestedBy: 5,
	}
	assert.True(t, request.PDFFactsAttested)
	assert.Equal(t, int64(4), request.Issuance.FaceAmountMinor)
	assert.ErrorIs(t, ErrInvoiceNotFound, ErrInvoiceNotFound)
	assert.False(t, errors.Is(ErrInvoiceNotFound, ErrInvoiceDocumentConflict))
}

func TestInvoicePaymentSourceContractDeclarations(t *testing.T) {
	claim := ClaimInvoiceTopUpsRequest{
		UserID:        11,
		ApplicationID: 22,
		TopUps: []TopUpVersionExpectation{{
			TopUpID:                33,
			ExpectedPaymentVersion: 1,
		}},
	}
	require.Len(t, claim.TopUps, 1)
	assert.Equal(t, 11, claim.UserID)
	assert.Equal(t, int64(22), claim.ApplicationID)
	assert.Equal(t, 33, claim.TopUps[0].TopUpID)
	assert.Equal(t, int64(1), claim.TopUps[0].ExpectedPaymentVersion)

	evidence := ClaimedTopUpEvidence{
		TopUpID:         33,
		MerchantTradeNo: "merchant-trade",
		PaidAmountMinor: 1200,
		Currency:        "CNY",
		ProductSnapshot: "product",
		PaidAt:          44,
		EvidenceSource:  "trusted_callback_v1",
		PaymentVersion:  2,
	}
	assert.Equal(t, int64(1200), evidence.PaidAmountMinor)
	assert.Equal(t, int64(2), evidence.PaymentVersion)

	release := ReleaseInvoiceTopUpsRequest{
		UserID:        claim.UserID,
		ApplicationID: claim.ApplicationID,
		TopUps:        []TopUpVersionExpectation{{TopUpID: 33, ExpectedPaymentVersion: 2}},
	}
	assert.Equal(t, int64(2), release.TopUps[0].ExpectedPaymentVersion)
	assert.Equal(t, ReleasedTopUpVersion{TopUpID: 33, PaymentVersion: 3}, ReleasedTopUpVersion{
		TopUpID:        33,
		PaymentVersion: 3,
	})

	require.Error(t, ErrInvoicePaymentSourceInvalidRequest)
	require.Error(t, ErrInvoicePaymentSourceNotEligible)
	require.Error(t, ErrInvoicePaymentSourceClaimConflict)
	require.Error(t, ErrInvoicePaymentSourceEvidenceConflict)
	assert.NotEqual(t, ErrInvoicePaymentSourceInvalidRequest, ErrInvoicePaymentSourceNotEligible)
	assert.NotEqual(t, ErrInvoicePaymentSourceClaimConflict, ErrInvoicePaymentSourceEvidenceConflict)
}
