package model

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type VerifiedEpayCompletion struct {
	MerchantTradeNo  string
	ProviderTradeNo  string
	ProviderTradeKey string
	Method           string
	PaidAmountMinor  int64
	AcceptedAt       int64
}

func NormalizeEpayProviderTradeIdentity(value string) (string, string, error) {
	if !utf8.ValidString(value) {
		return "", "", ErrVerifiedEpayCompletionInvalid
	}
	normalized := strings.TrimSpace(value)
	if size := len([]byte(normalized)); size == 0 || size > 191 {
		return "", "", ErrVerifiedEpayCompletionInvalid
	}
	hash := sha256.Sum256(append([]byte("epay\x00"), []byte(normalized)...))
	return normalized, hex.EncodeToString(hash[:]), nil
}

func invoicePaymentEvidenceMoneyMinor(money float64) (int64, error) {
	if math.IsNaN(money) || math.IsInf(money, 0) || money <= 0 {
		return 0, ErrVerifiedEpayCompletionInvalid
	}
	amount, err := decimal.NewFromString(strconv.FormatFloat(money, 'f', 2, 64))
	if err != nil {
		return 0, ErrVerifiedEpayCompletionInvalid
	}
	shifted := amount.Shift(2)
	if shifted.LessThanOrEqual(decimal.Zero) || !shifted.Equal(shifted.Truncate(0)) {
		return 0, ErrVerifiedEpayCompletionInvalid
	}
	minor := shifted.IntPart()
	if !decimal.NewFromInt(minor).Equal(shifted) {
		return 0, ErrVerifiedEpayCompletionInvalid
	}
	return minor, nil
}

func writeIneligibleInvoiceEvidence(topUp *TopUp) {
	eligible := false
	version := int64(1)
	topUp.InvoiceEligible, topUp.PaymentVersion = &eligible, &version
	topUp.InvoiceApplicationID, topUp.PaidAmountMinor, topUp.RefundedAmountMinor = nil, nil, nil
	topUp.Currency, topUp.PaymentState, topUp.ProductSnapshot = nil, nil, nil
	topUp.PaymentEvidenceSource, topUp.PaymentEvidenceRunID = nil, nil
	topUp.PaymentProviderTradeNo, topUp.PaymentProviderTradeKey = nil, nil
}

func validVerifiedEpayCompletion(completion VerifiedEpayCompletion) bool {
	providerTradeNo, providerTradeKey, err := NormalizeEpayProviderTradeIdentity(completion.ProviderTradeNo)
	return err == nil && providerTradeNo == completion.ProviderTradeNo && providerTradeKey == completion.ProviderTradeKey &&
		strings.TrimSpace(completion.MerchantTradeNo) != "" && strings.TrimSpace(completion.Method) != "" &&
		completion.PaidAmountMinor > 0 && completion.AcceptedAt > 0
}

func validateVerifiedEpayTopUp(topUp *TopUp, completion VerifiedEpayCompletion) error {
	if topUp.PaymentProvider != PaymentProviderEpay || topUp.PaymentMethod != completion.Method {
		return ErrPaymentMethodMismatch
	}
	if topUp.Status != common.TopUpStatusPending || topUp.PaymentVersion != nil || topUp.InvoiceApplicationID != nil ||
		topUp.PaidAmountMinor != nil || topUp.Currency != nil || topUp.PaymentState != nil ||
		topUp.RefundedAmountMinor != nil || topUp.ProductSnapshot != nil || topUp.PaymentEvidenceSource != nil ||
		topUp.PaymentEvidenceRunID != nil || topUp.PaymentProviderTradeNo != nil || topUp.PaymentProviderTradeKey != nil {
		return ErrVerifiedEpayCompletionConflict
	}
	expectedMinor, err := invoicePaymentEvidenceMoneyMinor(topUp.Money)
	if err != nil || expectedMinor != completion.PaidAmountMinor {
		return ErrVerifiedEpayCompletionConflict
	}
	return nil
}

func validateVerifiedEpayDatabaseConflicts(tx *gorm.DB, topUp *TopUp, providerTradeKey string) error {
	var subscriptionCount int64
	if err := tx.Model(&SubscriptionOrder{}).Where("trade_no = ?", topUp.TradeNo).Count(&subscriptionCount).Error; err != nil {
		return err
	}
	if subscriptionCount != 0 {
		return ErrVerifiedEpayCompletionConflict
	}
	var providerCount int64
	if err := tx.Model(&TopUp{}).Where("payment_provider_trade_key = ?", providerTradeKey).Count(&providerCount).Error; err != nil {
		return err
	}
	if providerCount != 0 {
		return ErrVerifiedEpayCompletionConflict
	}
	return nil
}

func writeTrustedInvoiceEvidence(topUp *TopUp, completion VerifiedEpayCompletion) {
	currency, eligible := constant.InvoicePaymentEvidenceCurrencyCNY, true
	paymentState, refunded := constant.InvoicePaymentStateSucceeded, int64(0)
	version, product := int64(1), constant.InvoicePaymentEvidenceTopUpProduct
	source := constant.InvoicePaymentEvidenceSourceTrustedCallback
	topUp.CompleteTime, topUp.PaidAmountMinor = completion.AcceptedAt, &completion.PaidAmountMinor
	topUp.Currency, topUp.InvoiceEligible = &currency, &eligible
	topUp.PaymentState, topUp.RefundedAmountMinor = &paymentState, &refunded
	topUp.PaymentVersion, topUp.ProductSnapshot = &version, &product
	topUp.PaymentEvidenceSource = &source
	topUp.PaymentProviderTradeNo, topUp.PaymentProviderTradeKey = &completion.ProviderTradeNo, &completion.ProviderTradeKey
}

func completeVerifiedEpayWalletTopUpTx(tx *gorm.DB, completion VerifiedEpayCompletion, creditResult **WalletTopUpCreditResult) error {
	var topUp TopUp
	if err := lockForUpdate(tx).Where("trade_no = ?", completion.MerchantTradeNo).First(&topUp).Error; err != nil {
		return normalizeTopUpLookupError(err)
	}
	if err := validateVerifiedEpayTopUp(&topUp, completion); err != nil {
		return err
	}
	if err := validateVerifiedEpayDatabaseConflicts(tx, &topUp, completion.ProviderTradeKey); err != nil {
		return err
	}
	quotaToAdd, err := calculateWalletTopUpQuota(&topUp)
	if err != nil {
		return err
	}
	*creditResult, err = creditWalletTopUpTx(tx, &topUp, quotaToAdd, PaymentProviderEpay)
	if err != nil {
		return err
	}
	writeTrustedInvoiceEvidence(&topUp, completion)
	return tx.Save(&topUp).Error
}

func CompleteVerifiedEpayWalletTopUp(completion VerifiedEpayCompletion) error {
	if !validVerifiedEpayCompletion(completion) {
		return ErrVerifiedEpayCompletionInvalid
	}
	var creditResult *WalletTopUpCreditResult
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return completeVerifiedEpayWalletTopUpTx(tx, completion, &creditResult)
	}); err != nil {
		return err
	}
	invalidateWalletTopUpQuotaCache(creditResult)
	return nil
}
