package router

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

type selfQuotaRouteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Quota int `json:"quota"`
	} `json:"data"`
}

func TestSelfQuotaRouteUsesAuthenticatedUserQuota(t *testing.T) {
	db := setupAccessTokenRouteDB(t)
	user := model.User{
		Id:          41,
		Username:    "quota-user",
		AuthVersion: 1,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       12345,
	}
	require.NoError(t, db.Create(&user).Error)
	token := createRouteScopedToken(t, user.Id, user.Role, "wallet:read")

	engine := gin.New()
	SetApiRouter(engine)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/user/self/quota", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("New-Api-User", "41")

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload selfQuotaRouteResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	assert.Equal(t, 12345, payload.Data.Quota)
}
