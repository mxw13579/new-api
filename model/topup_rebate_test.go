package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func migrateTopUpRebateForTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&AffiliateLog{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM affiliate_logs").Error)
	})
}

func withTopUpRebateSettings(t *testing.T, ratio float64, quotaPerUnit float64) {
	t.Helper()
	oldRatio := common.RechargeRebateRatioForInviter
	oldQuotaPerUnit := common.QuotaPerUnit
	common.RechargeRebateRatioForInviter = ratio
	common.QuotaPerUnit = quotaPerUnit
	t.Cleanup(func() {
		common.RechargeRebateRatioForInviter = oldRatio
		common.QuotaPerUnit = oldQuotaPerUnit
	})
}

func insertTopUpRebateUser(t *testing.T, id int, quota int, inviterId int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:        id,
		Username:  fmt.Sprintf("topup_rebate_user_%d", id),
		Status:    common.UserStatusEnabled,
		Quota:     quota,
		AffCode:   fmt.Sprintf("rebate_%d", id),
		InviterId: inviterId,
	}).Error)
}

func insertTopUpRebateOrder(t *testing.T, tradeNo string, userId int, amount int64, money float64, provider string) {
	t.Helper()
	require.NoError(t, DB.Create(&TopUp{
		UserId:          userId,
		Amount:          amount,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentMethod:   provider,
		PaymentProvider: provider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}).Error)
}

func getTopUpRebateUser(t *testing.T, id int) User {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, id).Error)
	return user
}

func countTopUpRebateLogs(t *testing.T, tradeNo string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&AffiliateLog{}).Where("trade_no = ?", tradeNo).Count(&count).Error)
	return count
}

func TestWalletTopUpRebateRatios(t *testing.T) {
	testCases := []struct {
		name       string
		ratio      float64
		wantRebate int
	}{
		{name: "disabled", ratio: 0, wantRebate: 0},
		{name: "five percent", ratio: 5, wantRebate: 50},
		{name: "full rebate", ratio: 100, wantRebate: 1000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			migrateTopUpRebateForTest(t)
			truncateTables(t)
			withTopUpRebateSettings(t, tc.ratio, 100)

			insertTopUpRebateUser(t, 1, 0, 0)
			insertTopUpRebateUser(t, 2, 10, 1)
			insertTopUpRebateOrder(t, "ratio-"+tc.name, 2, 10, 1, PaymentProviderEpay)

			err := CompleteEpayWalletTopUp("ratio-"+tc.name, PaymentProviderEpay, "")
			require.NoError(t, err)

			invitee := getTopUpRebateUser(t, 2)
			assert.Equal(t, 1010, invitee.Quota)

			inviter := getTopUpRebateUser(t, 1)
			assert.Equal(t, tc.wantRebate, inviter.AffQuota)
			assert.Equal(t, tc.wantRebate, inviter.AffHistoryQuota)
			assert.EqualValues(t, boolToInt64(tc.wantRebate > 0), countTopUpRebateLogs(t, "ratio-"+tc.name))
		})
	}
}

func TestWalletTopUpRebateNoInviter(t *testing.T) {
	migrateTopUpRebateForTest(t)
	truncateTables(t)
	withTopUpRebateSettings(t, 5, 100)

	insertTopUpRebateUser(t, 2, 10, 0)
	insertTopUpRebateOrder(t, "no-inviter", 2, 10, 1, PaymentProviderEpay)

	require.NoError(t, CompleteEpayWalletTopUp("no-inviter", PaymentProviderEpay, ""))

	invitee := getTopUpRebateUser(t, 2)
	assert.Equal(t, 1010, invitee.Quota)
	assert.Zero(t, countTopUpRebateLogs(t, "no-inviter"))
}

func TestWalletTopUpRejectsProviderMismatch(t *testing.T) {
	migrateTopUpRebateForTest(t)
	truncateTables(t)
	withTopUpRebateSettings(t, 5, 100)

	insertTopUpRebateUser(t, 2, 10, 0)
	insertTopUpRebateOrder(t, "provider-mismatch", 2, 10, 1, PaymentProviderStripe)

	err := CompleteEpayWalletTopUp("provider-mismatch", PaymentProviderEpay, "")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	topUp := GetTopUpByTradeNo("provider-mismatch")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 10, getTopUpRebateUser(t, 2).Quota)
}

func TestTopUpRebateLogMatchesAllowsTinyPercentDifference(t *testing.T) {
	existing := &AffiliateLog{
		InviterId:     1,
		InviteeId:     2,
		Type:          AffiliateLogTypeTopUpRebate,
		TradeNo:       "float-percent",
		RewardQuota:   50,
		BaseQuota:     1000,
		RebatePercent: 5.0000001,
	}
	expected := &AffiliateLog{
		InviterId:     1,
		InviteeId:     2,
		Type:          AffiliateLogTypeTopUpRebate,
		TradeNo:       "float-percent",
		RewardQuota:   50,
		BaseQuota:     1000,
		RebatePercent: 5.0000002,
	}

	assert.True(t, topUpRebateLogMatches(existing, expected))
}

func TestWalletTopUpDuplicateCompletionDoesNotDuplicateRebate(t *testing.T) {
	migrateTopUpRebateForTest(t)
	truncateTables(t)
	withTopUpRebateSettings(t, 5, 100)

	insertTopUpRebateUser(t, 1, 0, 0)
	insertTopUpRebateUser(t, 2, 10, 1)
	insertTopUpRebateOrder(t, "duplicate-complete", 2, 10, 1, PaymentProviderWaffo)

	require.NoError(t, RechargeWaffo("duplicate-complete", ""))
	require.NoError(t, RechargeWaffo("duplicate-complete", ""))

	invitee := getTopUpRebateUser(t, 2)
	assert.Equal(t, 1010, invitee.Quota)

	inviter := getTopUpRebateUser(t, 1)
	assert.Equal(t, 50, inviter.AffQuota)
	assert.Equal(t, 50, inviter.AffHistoryQuota)
	assert.EqualValues(t, 1, countTopUpRebateLogs(t, "duplicate-complete"))
}

func TestStripeWalletTopUpDuplicateRechargeDoesNotDuplicateRebate(t *testing.T) {
	migrateTopUpRebateForTest(t)
	truncateTables(t)
	withTopUpRebateSettings(t, 5, 100)

	insertTopUpRebateUser(t, 1, 0, 0)
	insertTopUpRebateUser(t, 2, 10, 1)
	insertTopUpRebateOrder(t, "stripe-duplicate-recharge", 2, 99, 2.5, PaymentProviderStripe)

	require.NoError(t, Recharge("stripe-duplicate-recharge", "cus_test", ""))
	require.NoError(t, Recharge("stripe-duplicate-recharge", "cus_test", ""))

	invitee := getTopUpRebateUser(t, 2)
	assert.Equal(t, 260, invitee.Quota)
	assert.Equal(t, "cus_test", invitee.StripeCustomer)

	inviter := getTopUpRebateUser(t, 1)
	assert.Equal(t, 12, inviter.AffQuota)
	assert.Equal(t, 12, inviter.AffHistoryQuota)
	assert.EqualValues(t, 1, countTopUpRebateLogs(t, "stripe-duplicate-recharge"))
}

func TestCreemWalletTopUpDuplicateRechargeDoesNotDuplicateRebate(t *testing.T) {
	migrateTopUpRebateForTest(t)
	truncateTables(t)
	withTopUpRebateSettings(t, 5, 100)

	insertTopUpRebateUser(t, 1, 0, 0)
	insertTopUpRebateUser(t, 2, 10, 1)
	insertTopUpRebateOrder(t, "creem-duplicate-recharge", 2, 500, 5, PaymentProviderCreem)

	require.NoError(t, RechargeCreem("creem-duplicate-recharge", "", "", ""))
	require.NoError(t, RechargeCreem("creem-duplicate-recharge", "", "", ""))

	invitee := getTopUpRebateUser(t, 2)
	assert.Equal(t, 510, invitee.Quota)

	inviter := getTopUpRebateUser(t, 1)
	assert.Equal(t, 25, inviter.AffQuota)
	assert.Equal(t, 25, inviter.AffHistoryQuota)
	assert.EqualValues(t, 1, countTopUpRebateLogs(t, "creem-duplicate-recharge"))
}

func TestCompletedWaffoProviderMismatchIsNotSilentlyIgnored(t *testing.T) {
	testCases := []struct {
		name     string
		tradeNo  string
		provider string
		run      func(string) error
	}{
		{
			name:     "waffo",
			tradeNo:  "waffo-success-provider-mismatch",
			provider: PaymentProviderStripe,
			run:      func(tradeNo string) error { return RechargeWaffo(tradeNo, "") },
		},
		{
			name:     "waffo pancake",
			tradeNo:  "waffo-pancake-success-provider-mismatch",
			provider: PaymentProviderStripe,
			run:      RechargeWaffoPancake,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			migrateTopUpRebateForTest(t)
			truncateTables(t)
			withTopUpRebateSettings(t, 5, 100)

			insertTopUpRebateUser(t, 2, 10, 0)
			insertTopUpRebateOrder(t, tc.tradeNo, 2, 10, 1, tc.provider)
			require.NoError(t, DB.Model(&TopUp{}).
				Where("trade_no = ?", tc.tradeNo).
				Update("status", common.TopUpStatusSuccess).Error)

			require.Error(t, tc.run(tc.tradeNo))
		})
	}
}

func TestManualCompleteTopUpIsIdempotent(t *testing.T) {
	migrateTopUpRebateForTest(t)
	truncateTables(t)
	withTopUpRebateSettings(t, 5, 100)

	insertTopUpRebateUser(t, 1, 0, 0)
	insertTopUpRebateUser(t, 2, 10, 1)
	insertTopUpRebateOrder(t, "manual-complete", 2, 10, 1, PaymentProviderEpay)

	require.NoError(t, ManualCompleteTopUp("manual-complete", ""))
	require.NoError(t, ManualCompleteTopUp("manual-complete", ""))

	assert.Equal(t, 1010, getTopUpRebateUser(t, 2).Quota)
	assert.Equal(t, 50, getTopUpRebateUser(t, 1).AffQuota)
	assert.EqualValues(t, 1, countTopUpRebateLogs(t, "manual-complete"))
}

func TestWalletTopUpRebateExistingLogOnPendingOrderFailsWithoutCredit(t *testing.T) {
	migrateTopUpRebateForTest(t)
	truncateTables(t)
	withTopUpRebateSettings(t, 5, 100)

	insertTopUpRebateUser(t, 1, 0, 0)
	insertTopUpRebateUser(t, 2, 10, 1)
	insertTopUpRebateOrder(t, "existing-log-pending", 2, 10, 1, PaymentProviderEpay)
	require.NoError(t, DB.Create(&AffiliateLog{
		InviterId:      1,
		InviteeId:      2,
		Type:           AffiliateLogTypeTopUpRebate,
		TradeNo:        "existing-log-pending",
		IdempotencyKey: TopUpRebateIdempotencyKey("existing-log-pending"),
		RewardQuota:    50,
		BaseQuota:      1000,
		RebatePercent:  5,
	}).Error)

	err := CompleteEpayWalletTopUp("existing-log-pending", PaymentProviderEpay, "")
	require.ErrorIs(t, err, ErrTopUpRebateConflict)

	topUp := GetTopUpByTradeNo("existing-log-pending")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 10, getTopUpRebateUser(t, 2).Quota)
	assert.Zero(t, getTopUpRebateUser(t, 1).AffQuota)
	assert.EqualValues(t, 1, countTopUpRebateLogs(t, "existing-log-pending"))
}

func TestWalletTopUpProviderQuotaBases(t *testing.T) {
	testCases := []struct {
		name     string
		provider string
		amount   int64
		money    float64
		complete func(string) error
		want     int
	}{
		{
			name:     "stripe uses money times quota per unit",
			provider: PaymentProviderStripe,
			amount:   99,
			money:    2.5,
			complete: func(tradeNo string) error { return Recharge(tradeNo, "cus_test", "") },
			want:     250,
		},
		{
			name:     "creem uses amount as quota",
			provider: PaymentProviderCreem,
			amount:   321,
			money:    9.99,
			complete: func(tradeNo string) error { return RechargeCreem(tradeNo, "", "", "") },
			want:     321,
		},
		{
			name:     "waffo uses amount times quota per unit",
			provider: PaymentProviderWaffo,
			amount:   3,
			money:    9.99,
			complete: func(tradeNo string) error { return RechargeWaffo(tradeNo, "") },
			want:     300,
		},
		{
			name:     "waffo pancake uses amount times quota per unit",
			provider: PaymentProviderWaffoPancake,
			amount:   4,
			money:    9.99,
			complete: RechargeWaffoPancake,
			want:     400,
		},
		{
			name:     "epay uses amount times quota per unit",
			provider: PaymentProviderEpay,
			amount:   5,
			money:    9.99,
			complete: func(tradeNo string) error { return CompleteEpayWalletTopUp(tradeNo, PaymentProviderEpay, "") },
			want:     500,
		},
		{
			name:     "manual completion uses original provider quota base",
			provider: PaymentProviderStripe,
			amount:   99,
			money:    6.5,
			complete: func(tradeNo string) error { return ManualCompleteTopUp(tradeNo, "") },
			want:     650,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			migrateTopUpRebateForTest(t)
			truncateTables(t)
			withTopUpRebateSettings(t, 5, 100)

			insertTopUpRebateUser(t, 1, 0, 0)
			insertTopUpRebateUser(t, 2, 10, 1)
			tradeNo := "quota-base-" + tc.name
			insertTopUpRebateOrder(t, tradeNo, 2, tc.amount, tc.money, tc.provider)

			require.NoError(t, tc.complete(tradeNo))

			assert.Equal(t, 10+tc.want, getTopUpRebateUser(t, 2).Quota)
			assert.Equal(t, tc.want*5/100, getTopUpRebateUser(t, 1).AffQuota)
		})
	}
}

func TestNonWalletTopUpQuotaChangesDoNotCreateTopUpRebateLogs(t *testing.T) {
	testCases := []struct {
		name string
		run  func(t *testing.T, userId int)
	}{
		{
			name: "check-in",
			run: func(t *testing.T, userId int) {
				require.NoError(t, DB.AutoMigrate(&Checkin{}))
				checkinSetting := operation_setting.GetCheckinSetting()
				oldEnabled := checkinSetting.Enabled
				oldMinQuota := checkinSetting.MinQuota
				oldMaxQuota := checkinSetting.MaxQuota
				checkinSetting.Enabled = true
				checkinSetting.MinQuota = 200
				checkinSetting.MaxQuota = 200
				t.Cleanup(func() {
					checkinSetting.Enabled = oldEnabled
					checkinSetting.MinQuota = oldMinQuota
					checkinSetting.MaxQuota = oldMaxQuota
				})

				_, err := UserCheckin(userId)
				require.NoError(t, err)
			},
		},
		{
			name: "redemption code",
			run: func(t *testing.T, userId int) {
				require.NoError(t, DB.AutoMigrate(&Redemption{}))
				require.NoError(t, DB.Create(&Redemption{
					UserId:      1,
					Key:         "12345678901234567890123456789012",
					Status:      common.RedemptionCodeStatusEnabled,
					Name:        "rebate guard",
					Quota:       300,
					CreatedTime: common.GetTimestamp(),
				}).Error)

				quota, err := Redeem("12345678901234567890123456789012", userId)
				require.NoError(t, err)
				assert.Equal(t, 300, quota)
			},
		},
		{
			name: "normal refund",
			run: func(t *testing.T, userId int) {
				require.NoError(t, IncreaseUserQuota(userId, 400, true))
			},
		},
		{
			name: "task refund",
			run: func(t *testing.T, userId int) {
				require.NoError(t, IncreaseUserQuota(userId, 500, false))
			},
		},
		{
			name: "admin direct quota edit",
			run: func(t *testing.T, userId int) {
				require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", 600).Error)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			migrateTopUpRebateForTest(t)
			truncateTables(t)
			withTopUpRebateSettings(t, 5, 100)

			insertTopUpRebateUser(t, 1, 0, 0)
			insertTopUpRebateUser(t, 2, 10, 1)

			tc.run(t, 2)

			var count int64
			require.NoError(t, DB.Model(&AffiliateLog{}).Where("type = ?", AffiliateLogTypeTopUpRebate).Count(&count).Error)
			assert.Zero(t, count)
			assert.Zero(t, getTopUpRebateUser(t, 1).AffQuota)
		})
	}
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
