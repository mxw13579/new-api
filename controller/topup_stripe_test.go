package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func TestStripeSessionCompletedReturnsErrorWhenWalletAccountingFails(t *testing.T) {
	db := setupEpayNotifyControllerTestDB(t)

	tradeNo := "stripe-accounting-failure"
	require.NoError(t, db.Create(&model.TopUp{
		UserId:          404,
		Amount:          10,
		Money:           1,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
	}).Error)

	err := sessionCompleted(context.Background(), stripe.Event{
		Type: stripe.EventTypeCheckoutSessionCompleted,
		Data: &stripe.EventData{
			Object: map[string]interface{}{
				"client_reference_id": tradeNo,
				"customer":            "cus_test",
				"status":              "complete",
				"payment_status":      "paid",
				"amount_total":        "100",
				"currency":            "usd",
			},
		},
	}, "")

	require.Error(t, err)
	var topUp model.TopUp
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&topUp).Error)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
}

func TestStripeSessionCompletedDuplicateWebhookIsIdempotent(t *testing.T) {
	db := setupEpayNotifyControllerTestDB(t)
	oldRatio := common.RechargeRebateRatioForInviter
	oldQuotaPerUnit := common.QuotaPerUnit
	common.RechargeRebateRatioForInviter = 5
	common.QuotaPerUnit = 100
	t.Cleanup(func() {
		common.RechargeRebateRatioForInviter = oldRatio
		common.QuotaPerUnit = oldQuotaPerUnit
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "stripe_inviter",
		Status:   common.UserStatusEnabled,
		AffCode:  "stripe_inviter",
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id:        2,
		Username:  "stripe_invitee",
		Status:    common.UserStatusEnabled,
		Quota:     10,
		InviterId: 1,
		AffCode:   "stripe_invitee",
	}).Error)

	tradeNo := "stripe-duplicate-webhook"
	require.NoError(t, db.Create(&model.TopUp{
		UserId:          2,
		Amount:          99,
		Money:           2.5,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
	}).Error)

	event := stripe.Event{
		Type: stripe.EventTypeCheckoutSessionCompleted,
		Data: &stripe.EventData{
			Object: map[string]interface{}{
				"client_reference_id": tradeNo,
				"customer":            "cus_test",
				"status":              "complete",
				"payment_status":      "paid",
				"amount_total":        "250",
				"currency":            "usd",
			},
		},
	}
	require.NoError(t, sessionCompleted(context.Background(), event, ""))
	require.NoError(t, sessionCompleted(context.Background(), event, ""))

	var invitee model.User
	require.NoError(t, db.First(&invitee, 2).Error)
	assert.Equal(t, 260, invitee.Quota)
	assert.Equal(t, "cus_test", invitee.StripeCustomer)

	var inviter model.User
	require.NoError(t, db.First(&inviter, 1).Error)
	assert.Equal(t, 12, inviter.AffQuota)
	assert.Equal(t, 12, inviter.AffHistoryQuota)

	var rebateLogs int64
	require.NoError(t, db.Model(&model.AffiliateLog{}).Where("trade_no = ?", tradeNo).Count(&rebateLogs).Error)
	assert.EqualValues(t, 1, rebateLogs)
}
