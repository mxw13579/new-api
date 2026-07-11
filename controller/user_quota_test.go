package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type selfQuotaResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Quota int `json:"quota"`
	} `json:"data"`
}

func TestGetSelfQuotaRejectsInvalidContextUserID(t *testing.T) {
	setupModelListControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 0)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/quota", nil)

	GetSelfQuota(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload selfQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	assert.False(t, payload.Success)
	assert.Equal(t, "invalid user id", payload.Message)
}

func TestGetSelfQuotaMissingUserKeepsZeroQuotaCompatibility(t *testing.T) {
	setupModelListControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 404)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/quota", nil)

	GetSelfQuota(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload selfQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, 0, payload.Data.Quota)
}
