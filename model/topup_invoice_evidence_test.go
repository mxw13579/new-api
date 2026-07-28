package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInvoiceEvidenceTopUpTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&AffiliateLog{}, &InvoicePaymentEvidenceBackfillRun{}, &InvoicePaymentEvidenceBackfillItem{}))
	withTopUpRebateSettings(t, 0, 100)
}

func insertInvoiceEvidenceUser(t *testing.T, id int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:       id,
		Username: fmt.Sprintf("invoice-evidence-user-%d", id),
		Status:   common.UserStatusEnabled,
		AffCode:  fmt.Sprintf("invoice-evidence-aff-%d", id),
	}).Error)
}

func insertPendingEpayTopUp(t *testing.T, tradeNo string, userID int, money float64) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:          userID,
		Amount:          10,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      100,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return topUp
}

func TestNormalizeEpayProviderTradeIdentity(t *testing.T) {
	normalized, key, err := NormalizeEpayProviderTradeIdentity("  Provider-CaSe  ")
	require.NoError(t, err)
	assert.Equal(t, "Provider-CaSe", normalized)
	assert.Equal(t, "771fcdffd2b65e65ee26ad50324072573d7cba9bc658d490d5bea09540c4b0ec", key)

	_, _, err = NormalizeEpayProviderTradeIdentity(strings.Repeat("界", 64))
	require.Error(t, err)

	_, _, err = NormalizeEpayProviderTradeIdentity(string([]byte{0xff}))
	require.Error(t, err)
}

func TestCompleteVerifiedEpayWalletTopUpWritesTrustedEvidenceAtomically(t *testing.T) {
	setupInvoiceEvidenceTopUpTest(t)
	insertInvoiceEvidenceUser(t, 301)
	topUp := insertPendingEpayTopUp(t, "merchant-trusted", 301, 1)
	providerTradeNo, providerKey, err := NormalizeEpayProviderTradeIdentity("  Provider-Trusted  ")
	require.NoError(t, err)

	err = CompleteVerifiedEpayWalletTopUp(VerifiedEpayCompletion{
		MerchantTradeNo:  topUp.TradeNo,
		ProviderTradeNo:  providerTradeNo,
		ProviderTradeKey: providerKey,
		Method:           "alipay",
		PaidAmountMinor:  100,
		AcceptedAt:       12345,
	})
	require.NoError(t, err)

	var got TopUp
	require.NoError(t, DB.First(&got, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Equal(t, int64(12345), got.CompleteTime)
	require.NotNil(t, got.PaidAmountMinor)
	assert.Equal(t, int64(100), *got.PaidAmountMinor)
	require.NotNil(t, got.Currency)
	assert.Equal(t, constant.InvoicePaymentEvidenceCurrencyCNY, *got.Currency)
	require.NotNil(t, got.InvoiceEligible)
	assert.True(t, *got.InvoiceEligible)
	assert.Nil(t, got.InvoiceApplicationID)
	require.NotNil(t, got.PaymentVersion)
	assert.Equal(t, int64(1), *got.PaymentVersion)
	require.NotNil(t, got.PaymentEvidenceSource)
	assert.Equal(t, constant.InvoicePaymentEvidenceSourceTrustedCallback, *got.PaymentEvidenceSource)
	assert.Nil(t, got.PaymentEvidenceRunID)
	require.NotNil(t, got.PaymentProviderTradeNo)
	assert.Equal(t, "Provider-Trusted", *got.PaymentProviderTradeNo)
	require.NotNil(t, got.PaymentProviderTradeKey)
	assert.Equal(t, providerKey, *got.PaymentProviderTradeKey)

	var user User
	require.NoError(t, DB.First(&user, 301).Error)
	assert.Equal(t, 1000, user.Quota)
	var logCount int64
	require.NoError(t, DB.Model(&Log{}).Where("user_id = ? AND type = ?", 301, LogTypeTopup).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)
}

func TestCompleteVerifiedEpayWalletTopUpRejectsTamperAndProviderCollision(t *testing.T) {
	setupInvoiceEvidenceTopUpTest(t)
	insertInvoiceEvidenceUser(t, 302)
	insertInvoiceEvidenceUser(t, 303)
	first := insertPendingEpayTopUp(t, "merchant-first", 302, 1)
	second := insertPendingEpayTopUp(t, "merchant-second", 303, 1)
	providerTradeNo, providerKey, err := NormalizeEpayProviderTradeIdentity("provider-collision")
	require.NoError(t, err)

	base := VerifiedEpayCompletion{
		MerchantTradeNo:  first.TradeNo,
		ProviderTradeNo:  providerTradeNo,
		ProviderTradeKey: providerKey,
		Method:           "alipay",
		PaidAmountMinor:  101,
		AcceptedAt:       12345,
	}
	require.Error(t, CompleteVerifiedEpayWalletTopUp(base))

	var unchanged TopUp
	require.NoError(t, DB.First(&unchanged, first.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, unchanged.Status)
	assert.Nil(t, unchanged.PaymentVersion)

	base.PaidAmountMinor = 100
	require.NoError(t, CompleteVerifiedEpayWalletTopUp(base))
	base.MerchantTradeNo = second.TradeNo
	require.Error(t, CompleteVerifiedEpayWalletTopUp(base))

	unchanged = TopUp{}
	require.NoError(t, DB.First(&unchanged, second.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, unchanged.Status)
	assert.Nil(t, unchanged.PaymentVersion)
}

func TestCompleteVerifiedEpayWalletTopUpRejectsSubscriptionMirror(t *testing.T) {
	setupInvoiceEvidenceTopUpTest(t)
	insertInvoiceEvidenceUser(t, 304)
	topUp := insertPendingEpayTopUp(t, "merchant-subscription", 304, 1)
	require.NoError(t, DB.Create(&SubscriptionOrder{TradeNo: topUp.TradeNo}).Error)
	providerTradeNo, providerKey, err := NormalizeEpayProviderTradeIdentity("provider-subscription")
	require.NoError(t, err)

	err = CompleteVerifiedEpayWalletTopUp(VerifiedEpayCompletion{
		MerchantTradeNo:  topUp.TradeNo,
		ProviderTradeNo:  providerTradeNo,
		ProviderTradeKey: providerKey,
		Method:           "alipay",
		PaidAmountMinor:  100,
		AcceptedAt:       12345,
	})
	require.Error(t, err)

	var got TopUp
	require.NoError(t, DB.First(&got, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
	assert.Nil(t, got.PaymentVersion)
}

func TestManualCompleteTopUpWritesAdminAttestedInvoiceEvidence(t *testing.T) {
	setupInvoiceEvidenceTopUpTest(t)
	insertInvoiceEvidenceUser(t, 305)
	topUp := insertPendingEpayTopUp(t, "merchant-manual", 305, 1)

	require.NoError(t, ManualCompleteTopUp(topUp.TradeNo, ""))

	var got TopUp
	require.NoError(t, DB.First(&got, topUp.Id).Error)
	require.NotNil(t, got.InvoiceEligible)
	assert.True(t, *got.InvoiceEligible)
	require.NotNil(t, got.PaymentVersion)
	assert.Equal(t, int64(1), *got.PaymentVersion)
	require.NotNil(t, got.PaidAmountMinor)
	assert.Equal(t, int64(100), *got.PaidAmountMinor)
	require.NotNil(t, got.Currency)
	assert.Equal(t, constant.InvoicePaymentEvidenceCurrencyCNY, *got.Currency)
	require.NotNil(t, got.PaymentState)
	assert.Equal(t, constant.InvoicePaymentStateSucceeded, *got.PaymentState)
	require.NotNil(t, got.RefundedAmountMinor)
	assert.Zero(t, *got.RefundedAmountMinor)
	require.NotNil(t, got.ProductSnapshot)
	assert.Equal(t, constant.InvoicePaymentEvidenceTopUpProduct, *got.ProductSnapshot)
	require.NotNil(t, got.PaymentEvidenceSource)
	assert.Equal(t, constant.InvoicePaymentEvidenceSourceAdminManualCompletion, *got.PaymentEvidenceSource)
	assert.Nil(t, got.PaymentEvidenceRunID)
	assert.Nil(t, got.PaymentProviderTradeNo)
	assert.Nil(t, got.PaymentProviderTradeKey)

	eligible, err := ListEligibleInvoiceTopUps(305, 1)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	assert.Equal(t, topUp.Id, eligible[0].Id)
}

func TestManualCompleteTopUpUpgradesExistingIneligibleBarrierWithoutCreditingAgain(t *testing.T) {
	setupInvoiceEvidenceTopUpTest(t)
	insertInvoiceEvidenceUser(t, 306)
	topUp := insertPendingEpayTopUp(t, "merchant-manual-existing", 306, 1)
	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = 12345
	writeIneligibleInvoiceEvidence(&topUp)
	require.NoError(t, DB.Save(&topUp).Error)

	require.NoError(t, ManualCompleteTopUp(topUp.TradeNo, ""))

	var got TopUp
	require.NoError(t, DB.First(&got, topUp.Id).Error)
	require.NotNil(t, got.InvoiceEligible)
	assert.True(t, *got.InvoiceEligible)
	require.NotNil(t, got.PaymentEvidenceSource)
	assert.Equal(t, constant.InvoicePaymentEvidenceSourceAdminManualCompletion, *got.PaymentEvidenceSource)
	var user User
	require.NoError(t, DB.First(&user, 306).Error)
	assert.Zero(t, user.Quota)
	eligible, err := ListEligibleInvoiceTopUps(306, 1)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	assert.Equal(t, topUp.Id, eligible[0].Id)
}

func TestCompleteVerifiedEpayWalletTopUpDuplicateCompletionConflicts(t *testing.T) {
	setupInvoiceEvidenceTopUpTest(t)
	insertInvoiceEvidenceUser(t, 306)
	topUp := insertPendingEpayTopUp(t, "merchant-duplicate", 306, 1)
	providerTradeNo, providerKey, err := NormalizeEpayProviderTradeIdentity("provider-duplicate")
	require.NoError(t, err)
	completion := VerifiedEpayCompletion{
		MerchantTradeNo:  topUp.TradeNo,
		ProviderTradeNo:  providerTradeNo,
		ProviderTradeKey: providerKey,
		Method:           "alipay",
		PaidAmountMinor:  100,
		AcceptedAt:       12345,
	}

	require.NoError(t, CompleteVerifiedEpayWalletTopUp(completion))
	require.Error(t, CompleteVerifiedEpayWalletTopUp(completion))
	var logCount int64
	require.NoError(t, DB.Model(&Log{}).Where("user_id = ? AND type = ?", 306, LogTypeTopup).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)
}

func TestVerifiedEpayCompletionRejectsMissingTopUp(t *testing.T) {
	setupInvoiceEvidenceTopUpTest(t)
	providerTradeNo, providerKey, err := NormalizeEpayProviderTradeIdentity("provider-missing")
	require.NoError(t, err)
	err = CompleteVerifiedEpayWalletTopUp(VerifiedEpayCompletion{
		MerchantTradeNo:  "missing",
		ProviderTradeNo:  providerTradeNo,
		ProviderTradeKey: providerKey,
		Method:           "alipay",
		PaidAmountMinor:  100,
		AcceptedAt:       12345,
	})
	assert.True(t, errors.Is(err, ErrTopUpNotFound) || errors.Is(err, gorm.ErrRecordNotFound))
}

func TestInvoiceEvidenceProviderIdentityAndFingerprintAreNotJSONVisible(t *testing.T) {
	topUp := trustedInvoiceTopUp(7001, 70, "json-hidden")
	payload, err := common.Marshal(topUp)
	require.NoError(t, err)
	assert.NotContains(t, string(payload), "payment_provider_trade_no")
	assert.NotContains(t, string(payload), "payment_provider_trade_key")

	itemPayload, err := common.Marshal(InvoicePaymentEvidenceBackfillItem{SourceFingerprint: strings.Repeat("a", 64)})
	require.NoError(t, err)
	assert.NotContains(t, string(itemPayload), "source_fingerprint")
	assert.NotContains(t, string(itemPayload), strings.Repeat("a", 64))
}
