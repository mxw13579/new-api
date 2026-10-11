package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInvoiceSettingsRouterTest(t *testing.T) (*gin.Engine, map[int]*model.User) {
	t.Helper()
	db := setupAccessTokenRouteDB(t)
	previousSetting := operation_setting.GetInvoiceSetting()
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		operation_setting.PublishInvoiceSetting(previousSetting)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
	users := make(map[int]*model.User)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		user := &model.User{
			Username: fmt.Sprintf("invoice-settings-%d", role), AuthVersion: 1,
			Role: role, Status: common.UserStatusEnabled, Group: "default", AffCode: fmt.Sprintf("settings-%d", role),
		}
		require.NoError(t, db.Create(user).Error)
		users[role] = user
	}
	engine := gin.New()
	SetApiRouter(engine)
	return engine, users
}

func performInvoiceSettingsRequest(engine *gin.Engine, token, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestInvoiceSettingsUseExistingOptionSurfaceWithNarrowAuthorization(t *testing.T) {
	engine, users := setupInvoiceSettingsRouterTest(t)
	routes := make(map[string]string)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = route.Handler
	}
	assert.Equal(t, "github.com/QuantumNous/new-api/controller.GetInvoiceSetting", routes[http.MethodGet+" /api/option/invoice"])
	assert.Equal(t, "github.com/QuantumNous/new-api/controller.UpdateInvoiceSetting", routes[http.MethodPut+" /api/option/invoice"])
	assert.NotContains(t, routes, http.MethodGet+" /api/admin/invoice/settings")
	assert.NotContains(t, routes, http.MethodPut+" /api/admin/invoice/settings")

	// Use real scoped PATs backed by the isolated fixture database, without
	// changing the legacy-token retirement deadline or authenticating externally.
	owner, admin, root := users[common.RoleCommonUser], users[common.RoleAdminUser], users[common.RoleRootUser]
	settingsScope := service.AccessTokenScopeOf(authz.InvoiceSettings)
	_, err := service.NormalizeAccessTokenScopes(owner.Id, owner.Role, []string{settingsScope})
	require.ErrorIs(t, err, service.ErrAccessTokenScopeForbidden)
	ownerToken := createRouteScopedToken(t, owner.Id, owner.Role, "profile:read")
	adminSettings := createRouteScopedToken(t, admin.Id, admin.Role, settingsScope)
	rootSettings := createRouteScopedToken(t, root.Id, root.Role, settingsScope)
	reviewer := createRouteScopedToken(t, admin.Id, admin.Role, service.AccessTokenScopeOf(authz.InvoiceReview))
	rootOptions := createRouteScopedToken(t, root.Id, root.Role, "option:read", "option:write")

	settingBody := `{"personal_enabled":true,"company_enabled":true,"application_window_days":45,"minimum_amount_minor":10000,"fee_percent":5,"pdf_retention_days":60,"r2_endpoint":"","r2_bucket":"","r2_access_key_id":"","r2_secret_access_key":""}`
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		body := ""
		if method == http.MethodPut {
			body = settingBody
		}
		for _, test := range []struct {
			name, token string
			status      int
			code        string
		}{
			{"common user", ownerToken, http.StatusForbidden, "AUTH_INSUFFICIENT_PRIVILEGE"},
			{"admin invoice settings", adminSettings, http.StatusOK, ""},
			{"root invoice settings", rootSettings, http.StatusOK, ""},
			{"review does not grant settings", reviewer, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
			{"root options do not grant invoice settings", rootOptions, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		} {
			t.Run(method+"/"+test.name, func(t *testing.T) {
				response := performInvoiceSettingsRequest(engine, test.token, method, "/api/option/invoice", body)
				assert.Equal(t, test.status, response.Code)
				if test.code != "" {
					assert.Contains(t, response.Body.String(), test.code)
					return
				}
				var payload struct {
					Success bool `json:"success"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
				assert.True(t, payload.Success)
			})
		}
	}
	for _, test := range []struct {
		name, token string
		status      int
		code        string
	}{
		{"admin cannot read all options", adminSettings, http.StatusForbidden, "AUTH_INSUFFICIENT_PRIVILEGE"},
		{"root invoice settings do not grant all options", rootSettings, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"root can read all options with option scope", rootOptions, http.StatusOK, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performInvoiceSettingsRequest(engine, test.token, http.MethodGet, "/api/option/", "")
			assert.Equal(t, test.status, response.Code)
			if test.code != "" {
				assert.Contains(t, response.Body.String(), test.code)
			}
		})
	}
}
