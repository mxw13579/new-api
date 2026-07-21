package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
