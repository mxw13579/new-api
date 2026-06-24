package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func migrateAffiliateLogsForTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&AffiliateLog{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM affiliate_logs").Error)
	})
}

func TestAffiliateLogIdempotencyKeys(t *testing.T) {
	assert.Equal(t, "invite_reward:10:20", InviteRewardIdempotencyKey(10, 20))
	assert.Equal(t, "topup_rebate:trade-123", TopUpRebateIdempotencyKey("trade-123"))
}

func TestGetAffiliateLogsByInviterFiltersAndPaginates(t *testing.T) {
	migrateAffiliateLogsForTest(t)

	require.NoError(t, DB.Create([]AffiliateLog{
		{
			InviterId:      1,
			InviteeId:      11,
			Type:           AffiliateLogTypeInviteReward,
			IdempotencyKey: InviteRewardIdempotencyKey(1, 11),
			RewardQuota:    100,
			CreatedAt:      100,
		},
		{
			InviterId:      1,
			InviteeId:      12,
			Type:           AffiliateLogTypeTopUpRebate,
			TradeNo:        "trade-12",
			IdempotencyKey: TopUpRebateIdempotencyKey("trade-12"),
			RewardQuota:    50,
			BaseQuota:      1000,
			RebatePercent:  5,
			CreatedAt:      200,
		},
		{
			InviterId:      2,
			InviteeId:      21,
			Type:           AffiliateLogTypeInviteReward,
			IdempotencyKey: InviteRewardIdempotencyKey(2, 21),
			RewardQuota:    100,
			CreatedAt:      300,
		},
	}).Error)

	page, err := GetAffiliateLogsByInviter(1, "", &common.PageInfo{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.EqualValues(t, 2, page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 1, page.PageSize)
	assert.Equal(t, "trade-12", page.Items[0].TradeNo)

	page, err = GetAffiliateLogsByInviter(1, AffiliateLogTypeInviteReward, &common.PageInfo{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.EqualValues(t, 1, page.Total)
	assert.Equal(t, 11, page.Items[0].InviteeId)
}

func TestInviteUserCreatesAffiliateLogAtomically(t *testing.T) {
	migrateAffiliateLogsForTest(t)
	truncateTables(t)

	oldQuotaForInviter := common.QuotaForInviter
	common.QuotaForInviter = 123
	t.Cleanup(func() {
		common.QuotaForInviter = oldQuotaForInviter
	})

	require.NoError(t, DB.Create(&User{Id: 1, Username: "inviter"}).Error)

	require.NoError(t, inviteUser(1, 2, common.QuotaForInviter))

	var inviter User
	require.NoError(t, DB.First(&inviter, 1).Error)
	assert.Equal(t, 1, inviter.AffCount)
	assert.Equal(t, 123, inviter.AffQuota)
	assert.Equal(t, 123, inviter.AffHistoryQuota)

	var log AffiliateLog
	require.NoError(t, DB.Where("idempotency_key = ?", InviteRewardIdempotencyKey(1, 2)).First(&log).Error)
	assert.Equal(t, AffiliateLogTypeInviteReward, log.Type)
	assert.Equal(t, 1, log.InviterId)
	assert.Equal(t, 2, log.InviteeId)
	assert.Equal(t, 123, log.RewardQuota)

	require.NoError(t, inviteUser(1, 2, common.QuotaForInviter))
	require.NoError(t, DB.First(&inviter, 1).Error)
	assert.Equal(t, 1, inviter.AffCount)
	assert.Equal(t, 123, inviter.AffQuota)
	assert.Equal(t, 123, inviter.AffHistoryQuota)
}

func TestCreateAffiliateLogIfNotExistsIsIdempotent(t *testing.T) {
	migrateAffiliateLogsForTest(t)
	truncateTables(t)

	log := &AffiliateLog{
		InviterId:      1,
		InviteeId:      2,
		Type:           AffiliateLogTypeInviteReward,
		IdempotencyKey: InviteRewardIdempotencyKey(1, 2),
		RewardQuota:    0,
	}

	inserted, err := createAffiliateLogIfNotExistsTx(DB, log)
	require.NoError(t, err)
	assert.True(t, inserted)

	inserted, err = createAffiliateLogIfNotExistsTx(DB, log)
	require.NoError(t, err)
	assert.False(t, inserted)

	var count int64
	require.NoError(t, DB.Model(&AffiliateLog{}).Where("idempotency_key = ?", log.IdempotencyKey).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestInviteUserCreatesZeroRewardLogAndCountsOnce(t *testing.T) {
	migrateAffiliateLogsForTest(t)
	truncateTables(t)

	require.NoError(t, DB.Create(&User{Id: 1, Username: "inviter", AffQuota: 77, AffHistoryQuota: 88}).Error)

	require.NoError(t, inviteUser(1, 2, 0))
	require.NoError(t, inviteUser(1, 2, 0))

	var inviter User
	require.NoError(t, DB.First(&inviter, 1).Error)
	assert.Equal(t, 1, inviter.AffCount)
	assert.Equal(t, 77, inviter.AffQuota)
	assert.Equal(t, 88, inviter.AffHistoryQuota)

	var log AffiliateLog
	require.NoError(t, DB.Where("idempotency_key = ?", InviteRewardIdempotencyKey(1, 2)).First(&log).Error)
	assert.Equal(t, AffiliateLogTypeInviteReward, log.Type)
	assert.Equal(t, 0, log.RewardQuota)
}

func TestUserInsertWithZeroInviterQuotaCreatesInviteRewardLog(t *testing.T) {
	migrateAffiliateLogsForTest(t)
	truncateTables(t)

	oldQuotaForInviter := common.QuotaForInviter
	oldQuotaForInvitee := common.QuotaForInvitee
	oldQuotaForNewUser := common.QuotaForNewUser
	paymentSetting := operation_setting.GetPaymentSetting()
	oldPaymentSetting := *paymentSetting
	t.Cleanup(func() {
		common.QuotaForInviter = oldQuotaForInviter
		common.QuotaForInvitee = oldQuotaForInvitee
		common.QuotaForNewUser = oldQuotaForNewUser
		*paymentSetting = oldPaymentSetting
	})
	common.QuotaForInviter = 0
	common.QuotaForInvitee = 0
	common.QuotaForNewUser = 0
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	require.NoError(t, DB.Create(&User{Id: 1, Username: "inviter"}).Error)

	invitee := &User{Username: "invitee", DisplayName: "Invitee"}
	require.NoError(t, invitee.Insert(1))

	var inviter User
	require.NoError(t, DB.First(&inviter, 1).Error)
	assert.Equal(t, 1, inviter.AffCount)
	assert.Equal(t, 0, inviter.AffQuota)
	assert.Equal(t, 0, inviter.AffHistoryQuota)

	var log AffiliateLog
	require.NoError(t, DB.Where("idempotency_key = ?", InviteRewardIdempotencyKey(1, invitee.Id)).First(&log).Error)
	assert.Equal(t, AffiliateLogTypeInviteReward, log.Type)
	assert.Equal(t, 0, log.RewardQuota)
}
