package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func ptr[T any](value T) *T { return &value }

func setupInvoicePaymentSourceTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&InvoicePaymentEvidenceBackfillRun{}, &InvoicePaymentEvidenceBackfillItem{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM invoice_payment_evidence_backfill_items")
		DB.Exec("DELETE FROM invoice_payment_evidence_backfill_runs")
	})
}

func trustedInvoiceTopUp(id, userID int, tradeNo string) TopUp {
	return TopUp{
		Id: id, UserId: userID, Amount: 10, Money: 1, TradeNo: tradeNo,
		PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CompleteTime: 1000, Status: common.TopUpStatusSuccess,
		PaidAmountMinor: ptr(int64(100)), Currency: ptr(constant.InvoicePaymentEvidenceCurrencyCNY),
		InvoiceEligible: ptr(true), PaymentState: ptr(constant.InvoicePaymentStateSucceeded),
		RefundedAmountMinor: ptr(int64(0)), PaymentVersion: ptr(int64(1)),
		ProductSnapshot:        ptr(constant.InvoicePaymentEvidenceTopUpProduct),
		PaymentEvidenceSource:  ptr(constant.InvoicePaymentEvidenceSourceTrustedCallback),
		PaymentProviderTradeNo: ptr("provider-" + tradeNo), PaymentProviderTradeKey: ptr(providerKeyForTest(tradeNo)),
	}
}

func providerKeyForTest(seed string) string {
	_, key, err := NormalizeEpayProviderTradeIdentity("provider-" + seed)
	if err != nil {
		panic(err)
	}
	return key
}

func createCompletedLegacyInvoiceTopUp(t *testing.T, id, userID int, tradeNo string) TopUp {
	t.Helper()
	run := InvoicePaymentEvidenceBackfillRun{
		PolicyVersion:       constant.InvoicePaymentEvidencePolicyVersion,
		CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256:        constant.InvoicePaymentEvidencePolicySHA256,
		Status:              InvoicePaymentEvidenceRunStatusCompleted,
		CompletedAt:         ptr(int64(2000)),
	}
	require.NoError(t, DB.Create(&run).Error)
	topUp := TopUp{
		Id: id, UserId: userID, Amount: 10, Money: 1, TradeNo: tradeNo,
		PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		CompleteTime: 1000, Status: common.TopUpStatusSuccess,
		PaidAmountMinor: ptr(int64(100)), Currency: ptr(constant.InvoicePaymentEvidenceCurrencyCNY),
		InvoiceEligible: ptr(true), PaymentState: ptr(constant.InvoicePaymentStateSucceeded),
		RefundedAmountMinor: ptr(int64(0)), PaymentVersion: ptr(int64(1)),
		ProductSnapshot:       ptr(constant.InvoicePaymentEvidenceTopUpProduct),
		PaymentEvidenceSource: ptr(constant.InvoicePaymentEvidenceSourceLegacyBackfill),
		PaymentEvidenceRunID:  ptr(run.ID),
	}
	require.NoError(t, DB.Create(&topUp).Error)
	fingerprint, amount, err := invoicePaymentEvidenceFingerprint(&topUp)
	require.NoError(t, err)
	require.Equal(t, int64(100), amount)
	require.NoError(t, DB.Create(&InvoicePaymentEvidenceBackfillItem{
		RunID: run.ID, TopUpID: topUp.Id, ExpectedAmountMinor: amount,
		SourceFingerprint: fingerprint, CreatedAt: 1500,
	}).Error)
	return topUp
}

func TestTopUpInvoicePaymentSourceClaimAndReleaseDeterministically(t *testing.T) {
	setupInvoicePaymentSourceTest(t)
	trusted := trustedInvoiceTopUp(4102, 41, "trusted-source")
	require.NoError(t, DB.Create(&trusted).Error)
	legacy := createCompletedLegacyInvoiceTopUp(t, 4101, 41, "legacy-source")
	source := NewTopUpInvoicePaymentSource()

	var claimed []ClaimedTopUpEvidence
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		claimed, err = source.ClaimTopUpsTx(tx, ClaimInvoiceTopUpsRequest{
			UserID: 41, ApplicationID: 9001,
			TopUps: []TopUpVersionExpectation{{TopUpID: trusted.Id, ExpectedPaymentVersion: 1}, {TopUpID: legacy.Id, ExpectedPaymentVersion: 1}},
		})
		return err
	}))
	require.Len(t, claimed, 2)
	assert.Equal(t, []int{legacy.Id, trusted.Id}, []int{claimed[0].TopUpID, claimed[1].TopUpID})
	assert.Equal(t, int64(2), claimed[0].PaymentVersion)
	assert.Equal(t, int64(2), claimed[1].PaymentVersion)

	var released []ReleasedTopUpVersion
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		released, err = source.ReleaseTopUpsTx(tx, ReleaseInvoiceTopUpsRequest{
			UserID: 41, ApplicationID: 9001,
			TopUps: []TopUpVersionExpectation{{TopUpID: trusted.Id, ExpectedPaymentVersion: 2}, {TopUpID: legacy.Id, ExpectedPaymentVersion: 2}},
		})
		return err
	}))
	assert.Equal(t, []ReleasedTopUpVersion{{TopUpID: legacy.Id, PaymentVersion: 3}, {TopUpID: trusted.Id, PaymentVersion: 3}}, released)
}

func TestTopUpInvoicePaymentSourceValidatesAndDoesNotPartiallyClaim(t *testing.T) {
	setupInvoicePaymentSourceTest(t)
	first := trustedInvoiceTopUp(4201, 42, "source-valid")
	second := trustedInvoiceTopUp(4202, 42, "source-ineligible")
	second.InvoiceEligible = ptr(false)
	require.NoError(t, DB.Create(&first).Error)
	require.NoError(t, DB.Create(&second).Error)
	source := NewTopUpInvoicePaymentSource()

	_, err := source.ClaimTopUpsTx(nil, ClaimInvoiceTopUpsRequest{})
	require.ErrorIs(t, err, ErrInvoicePaymentSourceInvalidRequest)
	_, err = source.ClaimTopUpsTx(DB, ClaimInvoiceTopUpsRequest{
		UserID: 42, ApplicationID: 9002,
		TopUps: []TopUpVersionExpectation{{TopUpID: first.Id, ExpectedPaymentVersion: 1}, {TopUpID: first.Id, ExpectedPaymentVersion: 1}},
	})
	require.ErrorIs(t, err, ErrInvoicePaymentSourceInvalidRequest)

	err = DB.Transaction(func(tx *gorm.DB) error {
		_, claimErr := source.ClaimTopUpsTx(tx, ClaimInvoiceTopUpsRequest{
			UserID: 42, ApplicationID: 9002,
			TopUps: []TopUpVersionExpectation{{TopUpID: first.Id, ExpectedPaymentVersion: 1}, {TopUpID: second.Id, ExpectedPaymentVersion: 1}},
		})
		return claimErr
	})
	require.ErrorIs(t, err, ErrInvoicePaymentSourceNotEligible)

	var unchanged TopUp
	require.NoError(t, DB.First(&unchanged, first.Id).Error)
	assert.Nil(t, unchanged.InvoiceApplicationID)
	require.NotNil(t, unchanged.PaymentVersion)
	assert.Equal(t, int64(1), *unchanged.PaymentVersion)
}

func TestTopUpInvoicePaymentSourceRejectsPriorClaimAndEvidenceDrift(t *testing.T) {
	setupInvoicePaymentSourceTest(t)
	claimed := trustedInvoiceTopUp(4301, 43, "source-claimed")
	claimed.InvoiceApplicationID = ptr(int64(9003))
	claimed.PaymentVersion = ptr(int64(2))
	require.NoError(t, DB.Create(&claimed).Error)
	source := NewTopUpInvoicePaymentSource()

	_, err := source.ClaimTopUpsTx(DB, ClaimInvoiceTopUpsRequest{
		UserID: 43, ApplicationID: 9003,
		TopUps: []TopUpVersionExpectation{{TopUpID: claimed.Id, ExpectedPaymentVersion: 1}},
	})
	require.ErrorIs(t, err, ErrInvoicePaymentSourceClaimConflict)

	claimed.InvoiceApplicationID = nil
	claimed.PaymentVersion = ptr(int64(1))
	claimed.PaidAmountMinor = ptr(int64(0))
	require.NoError(t, DB.Save(&claimed).Error)
	_, err = source.ClaimTopUpsTx(DB, ClaimInvoiceTopUpsRequest{
		UserID: 43, ApplicationID: 9003,
		TopUps: []TopUpVersionExpectation{{TopUpID: claimed.Id, ExpectedPaymentVersion: 1}},
	})
	require.ErrorIs(t, err, ErrInvoicePaymentSourceEvidenceConflict)
}
