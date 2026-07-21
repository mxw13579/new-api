package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpsertSubscriptionTopUpWritesIneligibleBarrier(t *testing.T) {
	truncateTables(t)
	order := &SubscriptionOrder{
		UserId:        41,
		Money:         12.34,
		TradeNo:       "subscription-invoice-barrier",
		PaymentMethod: "epay",
		CreateTime:    common.GetTimestamp() - 10,
		Status:        common.TopUpStatusSuccess,
	}

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return upsertSubscriptionTopUpTx(tx, order)
	}))

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&topUp).Error)
	require.NotNil(t, topUp.InvoiceEligible)
	assert.False(t, *topUp.InvoiceEligible)
	require.NotNil(t, topUp.PaymentVersion)
	assert.Equal(t, int64(1), *topUp.PaymentVersion)
	assert.Nil(t, topUp.InvoiceApplicationID)
	assert.Nil(t, topUp.PaidAmountMinor)
	assert.Nil(t, topUp.Currency)
	assert.Nil(t, topUp.PaymentState)
	assert.Nil(t, topUp.RefundedAmountMinor)
	assert.Nil(t, topUp.ProductSnapshot)
	assert.Nil(t, topUp.PaymentEvidenceSource)
	assert.Nil(t, topUp.PaymentEvidenceRunID)
	assert.Nil(t, topUp.PaymentProviderTradeNo)
	assert.Nil(t, topUp.PaymentProviderTradeKey)

	require.NoError(t, DB.Model(&topUp).Updates(map[string]any{
		"invoice_eligible":           true,
		"payment_version":            int64(77),
		"invoice_application_id":     int64(99),
		"paid_amount_minor":          int64(1234),
		"currency":                   "CNY",
		"payment_state":              "succeeded",
		"refunded_amount_minor":      int64(0),
		"product_snapshot":           "subscription",
		"payment_evidence_source":    "forbidden",
		"payment_evidence_run_id":    int64(88),
		"payment_provider_trade_no":  "provider-trade",
		"payment_provider_trade_key": "provider-key",
	}).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return upsertSubscriptionTopUpTx(tx, order)
	}))
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&topUp).Error)
	assert.False(t, *topUp.InvoiceEligible)
	assert.Equal(t, int64(1), *topUp.PaymentVersion)
	assert.Nil(t, topUp.InvoiceApplicationID)
	assert.Nil(t, topUp.PaidAmountMinor)
	assert.Nil(t, topUp.Currency)
	assert.Nil(t, topUp.PaymentState)
	assert.Nil(t, topUp.RefundedAmountMinor)
	assert.Nil(t, topUp.ProductSnapshot)
	assert.Nil(t, topUp.PaymentEvidenceSource)
	assert.Nil(t, topUp.PaymentEvidenceRunID)
	assert.Nil(t, topUp.PaymentProviderTradeNo)
	assert.Nil(t, topUp.PaymentProviderTradeKey)
}
