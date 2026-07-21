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

func resetInvoicePaymentSourceScenario(t *testing.T) {
	t.Helper()
	for _, table := range []string{"invoice_payment_evidence_backfill_items", "invoice_payment_evidence_backfill_runs", "top_ups", "subscription_orders"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error, table)
	}
}

func claimSingleInvoiceTopUp(topUpID int, userID int, applicationID int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		_, err := NewTopUpInvoicePaymentSource().ClaimTopUpsTx(tx, ClaimInvoiceTopUpsRequest{
			UserID: userID, ApplicationID: applicationID,
			TopUps: []TopUpVersionExpectation{{TopUpID: topUpID, ExpectedPaymentVersion: 1}},
		})
		return err
	})
}

func runTopUpInvoicePaymentSourceAcceptanceMatrix(t *testing.T) {
	t.Helper()
	source := NewTopUpInvoicePaymentSource()

	t.Run("invalid_requests", func(t *testing.T) {
		claimRequests := []ClaimInvoiceTopUpsRequest{
			{},
			{UserID: 1, ApplicationID: 1},
			{UserID: 0, ApplicationID: 1, TopUps: []TopUpVersionExpectation{{TopUpID: 1, ExpectedPaymentVersion: 1}}},
			{UserID: 1, ApplicationID: 0, TopUps: []TopUpVersionExpectation{{TopUpID: 1, ExpectedPaymentVersion: 1}}},
			{UserID: 1, ApplicationID: 1, TopUps: []TopUpVersionExpectation{{TopUpID: 0, ExpectedPaymentVersion: 1}}},
			{UserID: 1, ApplicationID: 1, TopUps: []TopUpVersionExpectation{{TopUpID: 1, ExpectedPaymentVersion: 2}}},
			{UserID: 1, ApplicationID: 1, TopUps: []TopUpVersionExpectation{{TopUpID: 1, ExpectedPaymentVersion: 1}, {TopUpID: 1, ExpectedPaymentVersion: 1}}},
		}
		for _, request := range claimRequests {
			_, err := source.ClaimTopUpsTx(DB, request)
			require.ErrorIs(t, err, ErrInvoicePaymentSourceInvalidRequest)
		}
		_, err := source.ClaimTopUpsTx(nil, ClaimInvoiceTopUpsRequest{})
		require.ErrorIs(t, err, ErrInvoicePaymentSourceInvalidRequest)
		_, err = source.ReleaseTopUpsTx(DB, ReleaseInvoiceTopUpsRequest{
			UserID: 1, ApplicationID: 1, TopUps: []TopUpVersionExpectation{{TopUpID: 1, ExpectedPaymentVersion: 1}},
		})
		require.ErrorIs(t, err, ErrInvoicePaymentSourceInvalidRequest)
	})

	t.Run("trusted_and_legacy_success", func(t *testing.T) {
		resetInvoicePaymentSourceScenario(t)
		trusted := trustedInvoiceTopUp(5102, 51, "matrix-trusted")
		require.NoError(t, DB.Create(&trusted).Error)
		legacy := createCompletedLegacyInvoiceTopUp(t, 5101, 51, "matrix-legacy")
		var claimed []ClaimedTopUpEvidence
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			var err error
			claimed, err = NewTopUpInvoicePaymentSource().ClaimTopUpsTx(tx, ClaimInvoiceTopUpsRequest{
				UserID: 51, ApplicationID: 9510,
				TopUps: []TopUpVersionExpectation{{TopUpID: trusted.Id, ExpectedPaymentVersion: 1}, {TopUpID: legacy.Id, ExpectedPaymentVersion: 1}},
			})
			return err
		}))
		require.Len(t, claimed, 2)
		assert.Equal(t, []int{legacy.Id, trusted.Id}, []int{claimed[0].TopUpID, claimed[1].TopUpID})
		var released []ReleasedTopUpVersion
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			var err error
			released, err = NewTopUpInvoicePaymentSource().ReleaseTopUpsTx(tx, ReleaseInvoiceTopUpsRequest{
				UserID: 51, ApplicationID: 9510,
				TopUps: []TopUpVersionExpectation{{TopUpID: trusted.Id, ExpectedPaymentVersion: 2}, {TopUpID: legacy.Id, ExpectedPaymentVersion: 2}},
			})
			return err
		}))
		assert.Equal(t, []ReleasedTopUpVersion{{TopUpID: legacy.Id, PaymentVersion: 3}, {TopUpID: trusted.Id, PaymentVersion: 3}}, released)
	})

	t.Run("claim_is_all_or_nothing", func(t *testing.T) {
		resetInvoicePaymentSourceScenario(t)
		first := trustedInvoiceTopUp(5111, 51, "matrix-atomic-claim-first")
		second := trustedInvoiceTopUp(5112, 51, "matrix-atomic-claim-second")
		second.InvoiceEligible = ptr(false)
		require.NoError(t, DB.Create(&first).Error)
		require.NoError(t, DB.Create(&second).Error)
		err := DB.Transaction(func(tx *gorm.DB) error {
			_, claimErr := source.ClaimTopUpsTx(tx, ClaimInvoiceTopUpsRequest{
				UserID: 51, ApplicationID: 9511,
				TopUps: []TopUpVersionExpectation{{TopUpID: first.Id, ExpectedPaymentVersion: 1}, {TopUpID: second.Id, ExpectedPaymentVersion: 1}},
			})
			return claimErr
		})
		require.ErrorIs(t, err, ErrInvoicePaymentSourceNotEligible)
		require.NoError(t, DB.First(&first, first.Id).Error)
		assert.Nil(t, first.InvoiceApplicationID)
		require.NotNil(t, first.PaymentVersion)
		assert.Equal(t, int64(1), *first.PaymentVersion)
	})

	t.Run("release_is_all_or_nothing", func(t *testing.T) {
		resetInvoicePaymentSourceScenario(t)
		first := trustedInvoiceTopUp(5121, 51, "matrix-atomic-release-first")
		second := trustedInvoiceTopUp(5122, 51, "matrix-atomic-release-second")
		first.InvoiceApplicationID, first.PaymentVersion = ptr(int64(9512)), ptr(int64(2))
		second.InvoiceApplicationID, second.PaymentVersion = ptr(int64(9999)), ptr(int64(2))
		require.NoError(t, DB.Create(&first).Error)
		require.NoError(t, DB.Create(&second).Error)
		err := DB.Transaction(func(tx *gorm.DB) error {
			_, releaseErr := source.ReleaseTopUpsTx(tx, ReleaseInvoiceTopUpsRequest{
				UserID: 51, ApplicationID: 9512,
				TopUps: []TopUpVersionExpectation{{TopUpID: first.Id, ExpectedPaymentVersion: 2}, {TopUpID: second.Id, ExpectedPaymentVersion: 2}},
			})
			return releaseErr
		})
		require.ErrorIs(t, err, ErrInvoicePaymentSourceClaimConflict)
		require.NoError(t, DB.First(&first, first.Id).Error)
		require.NotNil(t, first.InvoiceApplicationID)
		assert.Equal(t, int64(9512), *first.InvoiceApplicationID)
		require.NotNil(t, first.PaymentVersion)
		assert.Equal(t, int64(2), *first.PaymentVersion)
	})

	type trustedCase struct {
		name     string
		expected error
		mutate   func(*TopUp)
		after    func(*testing.T, *TopUp)
	}
	trustedCases := []trustedCase{
		{name: "wrong_user", expected: ErrInvoicePaymentSourceNotEligible, mutate: func(topUp *TopUp) { topUp.UserId++ }},
		{name: "status_not_success", expected: ErrInvoicePaymentSourceNotEligible, mutate: func(topUp *TopUp) { topUp.Status = common.TopUpStatusPending }},
		{name: "eligible_missing", expected: ErrInvoicePaymentSourceNotEligible, mutate: func(topUp *TopUp) { topUp.InvoiceEligible = nil }},
		{name: "eligible_false", expected: ErrInvoicePaymentSourceNotEligible, mutate: func(topUp *TopUp) { topUp.InvoiceEligible = ptr(false) }},
		{name: "completion_nonpositive", expected: ErrInvoicePaymentSourceNotEligible, mutate: func(topUp *TopUp) { topUp.CompleteTime = 0 }},
		{name: "subscription_mirror", expected: ErrInvoicePaymentSourceNotEligible, after: func(t *testing.T, topUp *TopUp) {
			require.NoError(t, DB.Create(&SubscriptionOrder{TradeNo: topUp.TradeNo}).Error)
		}},
		{name: "already_claimed", expected: ErrInvoicePaymentSourceClaimConflict, mutate: func(topUp *TopUp) { topUp.InvoiceApplicationID = ptr(int64(9511)) }},
		{name: "version_missing", expected: ErrInvoicePaymentSourceClaimConflict, mutate: func(topUp *TopUp) { topUp.PaymentVersion = nil }},
		{name: "version_conflict", expected: ErrInvoicePaymentSourceClaimConflict, mutate: func(topUp *TopUp) { topUp.PaymentVersion = ptr(int64(2)) }},
		{name: "paid_amount_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaidAmountMinor = nil }},
		{name: "paid_amount_nonpositive", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaidAmountMinor = ptr(int64(0)) }},
		{name: "currency_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.Currency = nil }},
		{name: "currency_conflict", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.Currency = ptr("USD") }},
		{name: "payment_state_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentState = nil }},
		{name: "payment_state_conflict", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentState = ptr("failed") }},
		{name: "refund_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.RefundedAmountMinor = nil }},
		{name: "refund_nonzero", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.RefundedAmountMinor = ptr(int64(1)) }},
		{name: "product_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.ProductSnapshot = nil }},
		{name: "product_blank", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.ProductSnapshot = ptr(" ") }},
		{name: "source_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentEvidenceSource = nil }},
		{name: "source_conflict", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentEvidenceSource = ptr("unknown") }},
		{name: "trusted_run_present", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentEvidenceRunID = ptr(int64(7)) }},
		{name: "provider_trade_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentProviderTradeNo = nil }},
		{name: "provider_key_missing", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentProviderTradeKey = nil }},
		{name: "provider_trade_not_normalized", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentProviderTradeNo = ptr(" provider ") }},
		{name: "provider_key_drift", expected: ErrInvoicePaymentSourceEvidenceConflict, mutate: func(topUp *TopUp) { topUp.PaymentProviderTradeKey = ptr("drift") }},
	}
	for _, testCase := range trustedCases {
		t.Run("trusted_"+testCase.name, func(t *testing.T) {
			resetInvoicePaymentSourceScenario(t)
			topUp := trustedInvoiceTopUp(5201, 52, "matrix-trusted-"+testCase.name)
			if testCase.mutate != nil {
				testCase.mutate(&topUp)
			}
			require.NoError(t, DB.Create(&topUp).Error)
			if testCase.after != nil {
				testCase.after(t, &topUp)
			}
			require.ErrorIs(t, claimSingleInvoiceTopUp(topUp.Id, 52, 9520), testCase.expected)
		})
	}

	type legacyCase struct {
		name   string
		mutate func(*testing.T, *TopUp, int64)
	}
	legacyCases := []legacyCase{
		{name: "run_reference_missing", mutate: func(t *testing.T, topUp *TopUp, _ int64) {
			require.NoError(t, DB.Model(topUp).Update("payment_evidence_run_id", nil).Error)
		}},
		{name: "run_missing", mutate: func(t *testing.T, topUp *TopUp, _ int64) {
			require.NoError(t, DB.Model(topUp).Update("payment_evidence_run_id", 999999).Error)
		}},
		{name: "run_previewed", mutate: func(t *testing.T, _ *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", runID).Update("status", InvoicePaymentEvidenceRunStatusPreviewed).Error)
		}},
		{name: "run_applying", mutate: func(t *testing.T, _ *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", runID).Update("status", InvoicePaymentEvidenceRunStatusApplying).Error)
		}},
		{name: "run_failed", mutate: func(t *testing.T, _ *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", runID).Update("status", InvoicePaymentEvidenceRunStatusFailed).Error)
		}},
		{name: "completed_at_missing", mutate: func(t *testing.T, _ *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", runID).Update("completed_at", nil).Error)
		}},
		{name: "policy_version_drift", mutate: func(t *testing.T, _ *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", runID).Update("policy_version", "drift").Error)
		}},
		{name: "canonical_policy_drift", mutate: func(t *testing.T, _ *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", runID).Update("canonical_policy_json", "{}").Error)
		}},
		{name: "policy_hash_drift", mutate: func(t *testing.T, _ *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", runID).Update("policy_sha256", "drift").Error)
		}},
		{name: "item_missing", mutate: func(t *testing.T, topUp *TopUp, runID int64) {
			require.NoError(t, DB.Where("run_id = ? AND topup_id = ?", runID, topUp.Id).Delete(&InvoicePaymentEvidenceBackfillItem{}).Error)
		}},
		{name: "item_amount_drift", mutate: func(t *testing.T, topUp *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillItem{}).Where("run_id = ? AND topup_id = ?", runID, topUp.Id).Update("expected_amount_minor", 101).Error)
		}},
		{name: "item_fingerprint_drift", mutate: func(t *testing.T, topUp *TopUp, runID int64) {
			require.NoError(t, DB.Model(&InvoicePaymentEvidenceBackfillItem{}).Where("run_id = ? AND topup_id = ?", runID, topUp.Id).Update("source_fingerprint", "drift").Error)
		}},
		{name: "source_fingerprint_drift", mutate: func(t *testing.T, topUp *TopUp, _ int64) {
			require.NoError(t, DB.Model(topUp).Update("payment_method", "wechat").Error)
		}},
		{name: "provider_trade_present", mutate: func(t *testing.T, topUp *TopUp, _ int64) {
			require.NoError(t, DB.Model(topUp).Update("payment_provider_trade_no", "unexpected").Error)
		}},
		{name: "provider_key_present", mutate: func(t *testing.T, topUp *TopUp, _ int64) {
			require.NoError(t, DB.Model(topUp).Update("payment_provider_trade_key", "unexpected").Error)
		}},
	}
	for _, testCase := range legacyCases {
		t.Run("legacy_"+testCase.name, func(t *testing.T) {
			resetInvoicePaymentSourceScenario(t)
			topUp := createCompletedLegacyInvoiceTopUp(t, 5301, 53, "matrix-legacy-"+testCase.name)
			require.NotNil(t, topUp.PaymentEvidenceRunID)
			testCase.mutate(t, &topUp, *topUp.PaymentEvidenceRunID)
			require.ErrorIs(t, claimSingleInvoiceTopUp(topUp.Id, 53, 9530), ErrInvoicePaymentSourceEvidenceConflict)
		})
	}

	releaseCases := []struct {
		name          string
		requestUser   int
		requestApp    int64
		mutate        func(*testing.T, *TopUp)
		expectedError error
	}{
		{name: "wrong_user", requestUser: 55, requestApp: 9540, expectedError: ErrInvoicePaymentSourceClaimConflict},
		{name: "wrong_application", requestUser: 54, requestApp: 9999, expectedError: ErrInvoicePaymentSourceClaimConflict},
		{name: "missing_owner", requestUser: 54, requestApp: 9540, mutate: func(t *testing.T, topUp *TopUp) {
			require.NoError(t, DB.Model(topUp).Update("invoice_application_id", nil).Error)
		}, expectedError: ErrInvoicePaymentSourceClaimConflict},
		{name: "wrong_version", requestUser: 54, requestApp: 9540, mutate: func(t *testing.T, topUp *TopUp) {
			require.NoError(t, DB.Model(topUp).Update("payment_version", 3).Error)
		}, expectedError: ErrInvoicePaymentSourceClaimConflict},
		{name: "evidence_drift", requestUser: 54, requestApp: 9540, mutate: func(t *testing.T, topUp *TopUp) {
			require.NoError(t, DB.Model(topUp).Update("product_snapshot", " ").Error)
		}, expectedError: ErrInvoicePaymentSourceEvidenceConflict},
		{name: "subscription_drift", requestUser: 54, requestApp: 9540, mutate: func(t *testing.T, topUp *TopUp) {
			require.NoError(t, DB.Create(&SubscriptionOrder{TradeNo: topUp.TradeNo}).Error)
		}, expectedError: ErrInvoicePaymentSourceEvidenceConflict},
	}
	for _, testCase := range releaseCases {
		t.Run("release_"+testCase.name, func(t *testing.T) {
			resetInvoicePaymentSourceScenario(t)
			topUp := trustedInvoiceTopUp(5401, 54, "matrix-release-"+testCase.name)
			topUp.InvoiceApplicationID = ptr(int64(9540))
			topUp.PaymentVersion = ptr(int64(2))
			require.NoError(t, DB.Create(&topUp).Error)
			if testCase.mutate != nil {
				testCase.mutate(t, &topUp)
			}
			err := DB.Transaction(func(tx *gorm.DB) error {
				_, releaseErr := NewTopUpInvoicePaymentSource().ReleaseTopUpsTx(tx, ReleaseInvoiceTopUpsRequest{
					UserID: testCase.requestUser, ApplicationID: testCase.requestApp,
					TopUps: []TopUpVersionExpectation{{TopUpID: topUp.Id, ExpectedPaymentVersion: 2}},
				})
				return releaseErr
			})
			require.ErrorIs(t, err, testCase.expectedError)
		})
	}
}

func TestTopUpInvoicePaymentSourceAcceptanceMatrix(t *testing.T) {
	setupInvoicePaymentSourceTest(t)
	runTopUpInvoicePaymentSourceAcceptanceMatrix(t)
}
