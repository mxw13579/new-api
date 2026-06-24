package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type affiliateLogsAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
		Page  int              `json:"page"`
	} `json:"data"`
	Message string `json:"message"`
}

func setupAffiliateLogControllerTestDB(t *testing.T) {
	t.Helper()

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.AffiliateLog{}))
}

func decodeAffiliateLogsResponse(t *testing.T, recorderBody []byte) affiliateLogsAPIResponse {
	t.Helper()

	var response affiliateLogsAPIResponse
	require.NoError(t, common.Unmarshal(recorderBody, &response))
	return response
}

func TestGetAffiliateLogsReturnsOnlyCurrentInviterLogs(t *testing.T) {
	setupAffiliateLogControllerTestDB(t)

	require.NoError(t, model.DB.Create([]model.User{
		{Id: 101, Username: "invitee-one", DisplayName: "Invitee One", AffCode: "aff101"},
		{Id: 102, Username: "invitee-two", AffCode: "aff102"},
		{Id: 201, Username: "other-invitee", DisplayName: "Other Invitee", AffCode: "aff201"},
	}).Error)

	require.NoError(t, model.DB.Create([]model.AffiliateLog{
		{
			InviterId:      10,
			InviteeId:      101,
			Type:           model.AffiliateLogTypeInviteReward,
			IdempotencyKey: model.InviteRewardIdempotencyKey(10, 101),
			RewardQuota:    100,
			CreatedAt:      100,
		},
		{
			InviterId:      10,
			InviteeId:      102,
			Type:           model.AffiliateLogTypeTopUpRebate,
			TradeNo:        "trade-current",
			IdempotencyKey: model.TopUpRebateIdempotencyKey("trade-current"),
			RewardQuota:    50,
			BaseQuota:      1000,
			RebatePercent:  5,
			CreatedAt:      200,
		},
		{
			InviterId:      20,
			InviteeId:      201,
			Type:           model.AffiliateLogTypeTopUpRebate,
			TradeNo:        "trade-other",
			IdempotencyKey: model.TopUpRebateIdempotencyKey("trade-other"),
			RewardQuota:    70,
			BaseQuota:      1000,
			RebatePercent:  7,
			CreatedAt:      300,
		},
	}).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/user/affiliate/logs?type=topup_rebate&page=1&page_size=20", nil, 10)
	GetAffiliateLogs(ctx)

	response := decodeAffiliateLogsResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success)
	assert.EqualValues(t, 1, response.Data.Total)
	assert.Equal(t, 1, response.Data.Page)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, "topup_rebate", response.Data.Items[0]["type"])
	assert.Equal(t, "invitee-two", response.Data.Items[0]["invitee_username"])
	assert.Equal(t, "invitee-two", response.Data.Items[0]["invitee_display_name"])
	assert.NotContains(t, response.Data.Items[0], "trade_no")
	assert.NotContains(t, response.Data.Items[0], "idempotency_key")
}

func TestGetAffiliateLogsSupportsPageQueryAlias(t *testing.T) {
	setupAffiliateLogControllerTestDB(t)

	require.NoError(t, model.DB.Create([]model.User{
		{Id: 101, Username: "invitee-one", DisplayName: "Invitee One", AffCode: "aff101"},
		{Id: 102, Username: "invitee-two", DisplayName: "Invitee Two", AffCode: "aff102"},
	}).Error)

	require.NoError(t, model.DB.Create([]model.AffiliateLog{
		{
			InviterId:      10,
			InviteeId:      101,
			Type:           model.AffiliateLogTypeInviteReward,
			IdempotencyKey: model.InviteRewardIdempotencyKey(10, 101),
			RewardQuota:    100,
			CreatedAt:      100,
		},
		{
			InviterId:      10,
			InviteeId:      102,
			Type:           model.AffiliateLogTypeTopUpRebate,
			TradeNo:        "trade-second",
			IdempotencyKey: model.TopUpRebateIdempotencyKey("trade-second"),
			RewardQuota:    50,
			BaseQuota:      1000,
			RebatePercent:  5,
			CreatedAt:      200,
		},
	}).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/user/affiliate/logs?page=2&page_size=1", nil, 10)
	GetAffiliateLogs(ctx)

	response := decodeAffiliateLogsResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success)
	assert.EqualValues(t, 2, response.Data.Total)
	assert.Equal(t, 2, response.Data.Page)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, float64(101), response.Data.Items[0]["invitee_id"])
	assert.Equal(t, "invitee-one", response.Data.Items[0]["invitee_username"])
	assert.Equal(t, "Invitee One", response.Data.Items[0]["invitee_display_name"])
}

func TestGetAffiliateLogsRejectsInvalidType(t *testing.T) {
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/user/affiliate/logs?type=bad", nil, 10)
	GetAffiliateLogs(ctx)

	response := decodeAffiliateLogsResponse(t, recorder.Body.Bytes())
	assert.False(t, response.Success)
	assert.Equal(t, "invalid affiliate log type", response.Message)
}
