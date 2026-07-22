package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInvoiceSettingsRouterTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	common.RedisEnabled = false
	common.IsMasterNode = true
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.CasbinRule{}, &model.AuthzRole{}, &model.Option{}))
	model.DB, model.LOG_DB = db, db
	previousSetting := *operation_setting.GetInvoiceSetting()
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	require.NoError(t, authz.Init(db))
	for id, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		token := fmt.Sprintf("invoice-settings-role-%d", role)
		require.NoError(t, db.Create(&model.User{
			Id: id + 1, Username: fmt.Sprintf("invoice-settings-%d", role), AccessToken: &token,
			Role: role, Status: common.UserStatusEnabled, Group: "default", AffCode: fmt.Sprintf("settings-%d", role),
		}).Error)
	}
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.IsMasterNode = previousRedis, previousMaster
		*operation_setting.GetInvoiceSetting() = previousSetting
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
	engine := gin.New()
	SetApiRouter(engine)
	return engine
}

func performInvoiceSettingsRequest(engine *gin.Engine, role int, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", fmt.Sprintf("Bearer invoice-settings-role-%d", role))
	request.Header.Set("New-Api-User", fmt.Sprintf("%d", map[int]int{
		common.RoleCommonUser: 1, common.RoleAdminUser: 2, common.RoleRootUser: 3,
	}[role]))
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestInvoiceSettingsUseExistingOptionSurfaceWithNarrowAuthorization(t *testing.T) {
	engine := setupInvoiceSettingsRouterTest(t)
	routes := make(map[string]string)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = route.Handler
	}
	assert.Equal(t, "github.com/QuantumNous/new-api/controller.GetInvoiceSetting", routes[http.MethodGet+" /api/option/invoice"])
	assert.Equal(t, "github.com/QuantumNous/new-api/controller.UpdateInvoiceSetting", routes[http.MethodPut+" /api/option/invoice"])
	assert.NotContains(t, routes, http.MethodGet+" /api/admin/invoice/settings")
	assert.NotContains(t, routes, http.MethodPut+" /api/admin/invoice/settings")

	settingBody := `{"personal_enabled":true,"company_enabled":true,"application_window_days":45,"minimum_amount_minor":100,"fee_quota":20,"pdf_retention_days":60}`
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		body := ""
		if method == http.MethodPut {
			body = settingBody
		}
		assert.Equal(t, http.StatusForbidden, performInvoiceSettingsRequest(engine, common.RoleCommonUser, method, "/api/option/invoice", body).Code)
		assert.Equal(t, http.StatusOK, performInvoiceSettingsRequest(engine, common.RoleAdminUser, method, "/api/option/invoice", body).Code)
		assert.Equal(t, http.StatusOK, performInvoiceSettingsRequest(engine, common.RoleRootUser, method, "/api/option/invoice", body).Code)
	}
	assert.Equal(t, http.StatusForbidden, performInvoiceSettingsRequest(engine, common.RoleAdminUser, http.MethodGet, "/api/option/", "").Code)
	assert.Equal(t, http.StatusOK, performInvoiceSettingsRequest(engine, common.RoleRootUser, http.MethodGet, "/api/option/", "").Code)
}
