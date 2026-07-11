package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type quotaDistributionResponse struct {
	Success bool                         `json:"success"`
	Message string                       `json:"message"`
	Data    []model.QuotaDistributionRow `json:"data"`
}

func setupDistributionControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.QuotaData{}))
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    1,
		Username:  "alice",
		TokenID:   11,
		UseGroup:  "default",
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    2,
		Username:  "bob",
		TokenID:   22,
		UseGroup:  "vip",
		ModelName: "gpt-b",
		CreatedAt: 1100,
		Count:     9,
		Quota:     900,
		TokenUsed: 400,
	}).Error)
}

func performDistributionRequest(t *testing.T, role int, userID int, target string) quotaDistributionResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", userID)
	ctx.Set("role", role)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)

	GetQuotaDistribution(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload quotaDistributionResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestGetQuotaDistributionRejectsInvalidRequestParameters(t *testing.T) {
	setupDistributionControllerTestDB(t)

	cases := []string{
		"/api/data/distribution?start_timestamp=bad&end_timestamp=2000&dimension=group",
		"/api/data/distribution?start_timestamp=1000&end_timestamp=bad&dimension=group",
		"/api/data/distribution?start_timestamp=2000&end_timestamp=1000&dimension=group",
		"/api/data/distribution?start_timestamp=1000&end_timestamp=2000&dimension=group&metric=money",
		"/api/data/distribution?start_timestamp=1000&end_timestamp=2000&dimension=node",
	}

	for _, target := range cases {
		t.Run(target, func(t *testing.T) {
			payload := performDistributionRequest(t, common.RoleCommonUser, 1, target)
			assert.False(t, payload.Success)
			assert.Empty(t, payload.Data)
		})
	}
}

func TestGetQuotaDistributionUserCannotWidenScopeWithQueryParameters(t *testing.T) {
	setupDistributionControllerTestDB(t)

	payload := performDistributionRequest(t, common.RoleCommonUser, 1, "/api/data/distribution?start_timestamp=1000&end_timestamp=2000&dimension=group&metric=quota&user_id=2&username=bob&role=100&scope=global&is_admin=true")

	require.True(t, payload.Success, payload.Message)
	require.Len(t, payload.Data, 1)
	assert.Equal(t, "group:default", payload.Data[0].ID)
	assert.Equal(t, int64(100), payload.Data[0].Quota)
	assert.Equal(t, int64(40), payload.Data[0].Tokens)
	assert.Equal(t, int64(2), payload.Data[0].Requests)
}

func TestGetQuotaDistributionAdminRejectsKeyDimension(t *testing.T) {
	setupDistributionControllerTestDB(t)

	payload := performDistributionRequest(t, common.RoleAdminUser, 10, "/api/data/distribution?start_timestamp=1000&end_timestamp=2000&dimension=key")

	assert.False(t, payload.Success)
	assert.Equal(t, "dimension not allowed", payload.Message)
	assert.Empty(t, payload.Data)
}

func TestGetQuotaDistributionRootUserDimensionUsesCurrentUserLabels(t *testing.T) {
	setupDistributionControllerTestDB(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice-current", Password: "password", AffCode: "aff-alice"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "", Password: "password", AffCode: "aff-blank"}).Error)

	payload := performDistributionRequest(t, common.RoleRootUser, 10, "/api/data/distribution?start_timestamp=1000&end_timestamp=2000&dimension=user&metric=quota")

	require.True(t, payload.Success, payload.Message)
	require.Len(t, payload.Data, 2)
	assert.Equal(t, "user:2", payload.Data[0].ID)
	assert.Equal(t, "user:2", payload.Data[0].Label)
	assert.Equal(t, "user:1", payload.Data[1].ID)
	assert.Equal(t, "alice-current", payload.Data[1].Label)
}

func TestGetQuotaDistributionRootAllowsKeyDimension(t *testing.T) {
	setupDistributionControllerTestDB(t)

	payload := performDistributionRequest(t, common.RoleRootUser, 10, "/api/data/distribution?start_timestamp=1000&end_timestamp=2000&dimension=key&metric=requests")

	require.True(t, payload.Success, payload.Message)
	require.Len(t, payload.Data, 2)
	assert.Equal(t, "key:22", payload.Data[0].ID)
	assert.Equal(t, int64(9), payload.Data[0].Requests)
	assert.Equal(t, "key:11", payload.Data[1].ID)
	assert.Equal(t, int64(2), payload.Data[1].Requests)
}
