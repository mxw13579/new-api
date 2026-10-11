package router

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// accessTokenExemptRoutes are /api routes that never reach dashboard
// authentication, so a personal access token has no rule to satisfy there.
var accessTokenExemptRoutes = []string{
	// Anonymous pages and setup.
	"GET /api/setup",
	"POST /api/setup",
	"GET /api/status",
	"GET /api/uptime/status",
	"GET /api/notice",
	"GET /api/user-agreement",
	"GET /api/privacy-policy",
	"GET /api/about",
	"GET /api/home_page_content",
	"GET /api/ratio_config",
	"GET /api/user/groups",

	// Anonymous account entry points.
	"GET /api/verification",
	"GET /api/reset_password",
	"POST /api/user/reset",
	"POST /api/user/register",
	"GET /api/user/login/encryption-key",
	"POST /api/user/login",
	"POST /api/user/login/2fa",
	"POST /api/user/login/verify",
	"POST /api/user/login/passkey/begin",
	"POST /api/user/login/passkey/finish",
	"POST /api/user/passkey/login/begin",
	"POST /api/user/passkey/login/finish",
	"GET /api/oauth/wechat",
	"GET /api/oauth/telegram/login",
	"GET /api/oauth/telegram/bind/:flow_token",

	// Browser session cookie endpoints.
	"POST /api/user/auth/refresh",
	"POST /api/user/auth/logout",

	// Payment callbacks.
	"POST /api/stripe/webhook",
	"POST /api/creem/webhook",
	"POST /api/waffo/webhook",
	"POST /api/waffo-pancake/webhook/:env",
	"GET /api/user/epay/notify",
	"POST /api/user/epay/notify",
	"GET /api/subscription/epay/notify",
	"POST /api/subscription/epay/notify",
	"GET /api/subscription/epay/return",
	"POST /api/subscription/epay/return",

	// API key authentication (TokenAuthReadOnly).
	"GET /api/usage/token/",
	"GET /api/log/token",
}

// accessTokenHelperRoutes are declared through handlePermissionRoute outside
// channelPermissionRoutes.
var accessTokenHelperRoutes = map[string]authz.Permission{
	"GET /api/task_plugin_options": authz.TaskPluginBind,
	"GET /api/audit":               authz.AuditRead,
}

func newAccessTokenScopeTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// A permission route that is also in the static table panics here.
	require.NotPanics(t, func() {
		SetApiRouter(engine)
		SetDashboardRouter(engine)
		SetRelayRouter(engine)
		SetTaskPluginProtocolRouter(engine)
		SetVideoRouter(engine)
		SetTaskRouter(engine)
	})
	return engine
}

func TestAccessTokenRouteRulesCoverEveryDashboardRoute(t *testing.T) {
	engine := newAccessTokenScopeTestEngine(t)
	exempt := map[string]bool{}
	for _, key := range accessTokenExemptRoutes {
		exempt[key] = true
	}
	registered := map[string]bool{}
	panel := map[string]bool{}
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/pg/") {
			continue
		}
		key := route.Method + " " + route.Path
		registered[key] = true
		if !exempt[key] {
			panel[key] = true
		}
	}
	for _, key := range accessTokenExemptRoutes {
		assert.True(t, registered[key], "exempt route %s is no longer registered", key)
	}

	declared := map[string]int{}
	for _, key := range middleware.AccessTokenRouteRuleKeys() {
		declared[key]++
	}
	for key := range panel {
		assert.Equal(t, 1, declared[key], "panel route %s must be declared exactly once", key)
	}
	for key := range declared {
		assert.True(t, panel[key], "declared route %s is not a registered panel route", key)
	}

	permissionRoutes := map[string]authz.Permission{}
	for _, route := range channelPermissionRoutes {
		permissionRoutes[route.method+" "+joinPaths("/api/channel", route.path)] = route.permission
	}
	for key, permission := range accessTokenHelperRoutes {
		permissionRoutes[key] = permission
	}
	for _, route := range invoicePaymentEvidenceRoutes {
		permissionRoutes[route.method+" "+joinPaths("/api/system-task", route.path)] = route.permission
	}
	for key, permission := range permissionRoutes {
		rule, ok := middleware.AccessTokenRouteRule(key)
		require.True(t, ok, key)
		assert.Equal(t, "scope", rule.Kind(), key)
		assert.Equal(t, service.AccessTokenScopeOf(permission), rule.Scope(), key)
	}
}

func TestAccessTokenCatalogCoversEveryPermission(t *testing.T) {
	catalog := service.AccessTokenCatalog(1, common.RoleRootUser)
	scopes := map[string]bool{}
	resources := map[string]int{}
	for _, group := range catalog {
		for _, resource := range group.Resources {
			resources[resource.Resource]++
			for _, action := range resource.Actions {
				scopes[resource.Resource+":"+action.Action] = true
			}
		}
	}
	for _, permission := range authz.AllPermissions() {
		assert.True(t, scopes[service.AccessTokenScopeOf(permission)], "root catalog lacks %s", service.AccessTokenScopeOf(permission))
	}
	for resource, count := range resources {
		assert.Equal(t, 1, count, "resource %s appears in more than one catalog entry", resource)
	}
	newAccessTokenScopeTestEngine(t)
	for _, key := range middleware.AccessTokenRouteRuleKeys() {
		rule, _ := middleware.AccessTokenRouteRule(key)
		if rule.Kind() == "scope" {
			assert.True(t, scopes[rule.Scope()], "route %s requires an ungrantable scope %s", key, rule.Scope())
		}
	}
}

func TestAccessTokenVerificationScopesAreAllListed(t *testing.T) {
	files, err := filepath.Glob("../service/*.go")
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	var scopes []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, file, nil, 0)
		require.NoError(t, err)
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if !strings.HasPrefix(name.Name, "VerificationScope") || i >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[i].(*ast.BasicLit)
					require.True(t, ok, name.Name)
					scope, err := strconv.Unquote(literal.Value)
					require.NoError(t, err)
					scopes = append(scopes, scope)
				}
			}
		}
	}
	require.NotEmpty(t, scopes)
	for _, scope := range scopes {
		_, listed := service.AccessTokenVerificationScope(scope)
		assert.True(t, listed, "verification scope %s has no access token rule", scope)
	}
}

// All identities and data are synthetic; no login, challenge, payment, or
// object-storage service is called by these route tests.
func setupAccessTokenRouteDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	previousSecret := common.SessionSecret
	previousGlobalLimit, previousCriticalLimit := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.IsMasterNode = false, true
	common.SessionSecret = "synthetic-router-session-signing-key"
	common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled, common.IsMasterNode = previousRedis, previousMaster
		common.SessionSecret = previousSecret
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = previousGlobalLimit, previousCriticalLimit
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.Option{}, &model.AuditLog{}, &model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	return db
}

func invoiceRouteSessionToken(t *testing.T, userID int) string {
	t.Helper()
	bundle, err := service.CreateLoginSession(userID, "route-fixture", "127.0.0.1", "isolated-route-test")
	require.NoError(t, err)
	return bundle.AccessToken
}

func createRouteScopedToken(t *testing.T, userID, role int, scopes ...string) string {
	t.Helper()
	grant, err := service.NormalizeAccessTokenScopes(userID, role, scopes)
	require.NoError(t, err)
	suffix, err := common.GenerateRandomCharsKey(43)
	require.NoError(t, err)
	raw := model.AccessTokenPrefix + suffix
	token := &model.UserAccessToken{
		Name: "synthetic route token", TokenHash: model.AccessTokenFingerprint(raw),
		TokenHint: model.AccessTokenHint(raw), ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, token.SetScopes(grant))
	require.NoError(t, model.CreateUserAccessToken(userID, token, service.AccessTokenMaxPerUser))
	return raw
}

func TestAccessTokenDistributionGrantsBoundDataScope(t *testing.T) {
	db := setupAccessTokenRouteDB(t)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}, &model.Token{}))
	other := model.User{Username: "distribution-other", Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "distribution-other", AuthVersion: 1}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&model.QuotaData{UserID: other.Id, Username: other.Username, TokenID: 1,
		UseGroup: "other-group", CreatedAt: 10, Quota: 900, TokenUsed: 400, Count: 9}).Error)
	engine := gin.New()
	SetApiRouter(engine)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			user := model.User{Username: "distribution-" + strconv.Itoa(role), Role: role, Status: common.UserStatusEnabled,
				Group: "default", AffCode: "distribution-" + strconv.Itoa(role), AuthVersion: 1}
			require.NoError(t, db.Create(&user).Error)
			group := "self-" + strconv.Itoa(role)
			require.NoError(t, db.Create(&model.QuotaData{UserID: user.Id, Username: user.Username, TokenID: role,
				UseGroup: group, CreatedAt: 10, Quota: 100, TokenUsed: 40, Count: 2}).Error)
			usage := createRouteScopedToken(t, user.Id, role, "usage:read")
			session := invoiceRouteSessionToken(t, user.Id)
			logs := ""
			if role >= common.RoleAdminUser {
				logs = createRouteScopedToken(t, user.Id, role, "log:read")
			}
			for _, test := range []struct {
				name, token, query string
				global             bool
			}{
				{name: "personal grant ignores widening parameters", token: usage, query: "&username=" + other.Username + "&user_id=" + strconv.Itoa(other.Id) + "&role=100&scope=global&is_admin=true"},
				{name: "browser preserves role and username filter", token: session, query: "&username=" + other.Username, global: role >= common.RoleAdminUser},
				{name: "cross-user grant", token: logs, query: "&username=" + other.Username, global: true},
			} {
				if test.token == "" {
					continue
				}
				t.Run(test.name, func(t *testing.T) {
					response := performInvoiceRouteRequest(engine, user.Id, test.token, http.MethodGet,
						"/api/data/distribution?start_timestamp=1&end_timestamp=20&dimension=group"+test.query, "")
					require.Equal(t, http.StatusOK, response.Code)
					var payload struct {
						Success bool                         `json:"success"`
						Data    []model.QuotaDistributionRow `json:"data"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
					require.True(t, payload.Success)
					want := model.QuotaDistributionRow{ID: "group:" + group, Label: group, Dimension: "group", Quota: 100, Tokens: 40, Requests: 2}
					if test.global {
						want = model.QuotaDistributionRow{ID: "group:other-group", Label: "other-group", Dimension: "group", Quota: 900, Tokens: 400, Requests: 9}
					}
					assert.Equal(t, []model.QuotaDistributionRow{want}, payload.Data)
				})
			}
			if role == common.RoleAdminUser {
				// The log grant does not give admins the root-only global key view.
				response := performInvoiceRouteRequest(engine, user.Id, logs, http.MethodGet,
					"/api/data/distribution?start_timestamp=1&end_timestamp=20&dimension=key", "")
				assert.Contains(t, response.Body.String(), `"success":false`)
				assert.Contains(t, response.Body.String(), "dimension not allowed")
			}
			if role == common.RoleRootUser {
				// A stored cross-user grant cannot outlive its account's role.
				require.NoError(t, db.Model(&user).Update("role", common.RoleCommonUser).Error)
				response := performInvoiceRouteRequest(engine, user.Id, logs, http.MethodGet,
					"/api/data/distribution?start_timestamp=1&end_timestamp=20&dimension=group", "")
				assert.Equal(t, http.StatusForbidden, response.Code)
				assert.Contains(t, response.Body.String(), "ACCESS_TOKEN_SCOPE_DENIED")
			}
		})
	}
}

func TestAccessTokenCustomRoutesEnforceLeastPrivilege(t *testing.T) {
	db := setupAccessTokenRouteDB(t)
	// Only read endpoints and invalid requests reach business handlers.
	require.NoError(t, db.AutoMigrate(&model.InvoiceProfile{}, &model.AffiliateLog{}, &model.QuotaData{}))
	users := make(map[int]*model.User)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		user := &model.User{Username: "route-user-" + strconv.Itoa(role), Role: role, Status: common.UserStatusEnabled,
			Group: "default", AffCode: "route-aff-" + strconv.Itoa(role), AuthVersion: 1}
		require.NoError(t, db.Create(user).Error)
		users[role] = user
	}
	owner, admin, root := users[common.RoleCommonUser], users[common.RoleAdminUser], users[common.RoleRootUser]
	walletRead := createRouteScopedToken(t, owner.Id, owner.Role, "wallet:read")
	walletWrite := createRouteScopedToken(t, owner.Id, owner.Role, "wallet:write")
	profileRead := createRouteScopedToken(t, owner.Id, owner.Role, "profile:read")
	reviewer := createRouteScopedToken(t, admin.Id, admin.Role, service.AccessTokenScopeOf(authz.InvoiceReview))
	settings := createRouteScopedToken(t, admin.Id, admin.Role, service.AccessTokenScopeOf(authz.InvoiceSettings))
	billing := createRouteScopedToken(t, admin.Id, admin.Role, "billing:read", "billing:write")
	usage := createRouteScopedToken(t, root.Id, root.Role, "usage:read")
	logs := createRouteScopedToken(t, root.Id, root.Role, "log:read")
	ops := createRouteScopedToken(t, root.Id, root.Role, "ops:read", "ops:write")
	evidenceRead := createRouteScopedToken(t, root.Id, root.Role, service.AccessTokenScopeOf(authz.InvoicePaymentEvidenceRead))
	evidenceOperate := createRouteScopedToken(t, root.Id, root.Role, service.AccessTokenScopeOf(authz.InvoicePaymentEvidenceOperate))
	userWrite := createRouteScopedToken(t, root.Id, root.Role, "user:write")

	engine := gin.New()
	SetApiRouter(engine)
	engine.GET("/api/synthetic-undeclared", middleware.UserAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, test := range []struct {
		name, method, path, token string
		status                    int
		code                      string
	}{
		{"quota wallet read", http.MethodGet, "/api/user/self/quota", walletRead, http.StatusOK, ""},
		{"quota rejects profile scope", http.MethodGet, "/api/user/self/quota", profileRead, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"affiliate wallet read", http.MethodGet, "/api/user/affiliate/logs", walletRead, http.StatusOK, ""},
		{"affiliate rejects profile scope", http.MethodGet, "/api/user/affiliate/logs", profileRead, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"invoice wallet read", http.MethodGet, "/api/user/invoice/profiles", walletRead, http.StatusOK, ""},
		{"invoice rejects profile scope", http.MethodGet, "/api/user/invoice/profiles", profileRead, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"wallet read cannot write invoices", http.MethodPost, "/api/user/invoice/profiles", walletRead, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"wallet write reaches validation", http.MethodPost, "/api/user/invoice/profiles", walletWrite, http.StatusBadRequest, ""},
		{"wallet write does not imply read", http.MethodGet, "/api/user/invoice/profiles", walletWrite, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"admin review reaches validation", http.MethodGet, "/api/admin/invoices?page_size=101", reviewer, http.StatusBadRequest, constant.InvoiceCodeInvalidRequest},
		{"billing is not invoice review", http.MethodGet, "/api/admin/invoices?page_size=101", billing, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"invoice settings permitted", http.MethodGet, "/api/option/invoice", settings, http.StatusOK, ""},
		{"review is not settings", http.MethodGet, "/api/option/invoice", reviewer, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"personal wallet is not admin", http.MethodGet, "/api/admin/invoices", walletRead, http.StatusForbidden, "AUTH_INSUFFICIENT_PRIVILEGE"},
		{"root personal usage reads only self distribution", http.MethodGet, "/api/data/distribution?dimension=group&start_timestamp=1&end_timestamp=2", usage, http.StatusOK, ""},
		{"distribution rejects unrelated scope", http.MethodGet, "/api/data/distribution?dimension=group&start_timestamp=1&end_timestamp=2", profileRead, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"global distribution log scope", http.MethodGet, "/api/data/distribution?dimension=group&start_timestamp=1&end_timestamp=2", logs, http.StatusOK, ""},
		{"ops is not evidence read", http.MethodGet, "/api/system-task/invoice-payment-evidence/backfill/invalid", ops, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"evidence read reaches validation", http.MethodGet, "/api/system-task/invoice-payment-evidence/backfill/invalid", evidenceRead, http.StatusBadRequest, constant.InvoicePaymentEvidenceCodeInvalidRequest},
		{"evidence read cannot apply", http.MethodPost, "/api/system-task/invoice-payment-evidence/backfill/invalid/apply", evidenceRead, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"evidence operate reaches validation", http.MethodPost, "/api/system-task/invoice-payment-evidence/backfill/invalid/apply", evidenceOperate, http.StatusBadRequest, constant.InvoicePaymentEvidenceCodeInvalidRequest},
		{"evidence operate does not imply read", http.MethodGet, "/api/system-task/invoice-payment-evidence/backfill/invalid", evidenceOperate, http.StatusForbidden, "ACCESS_TOKEN_SCOPE_DENIED"},
		{"new routes remain default deny", http.MethodGet, "/api/synthetic-undeclared", logs, http.StatusForbidden, "ACCESS_TOKEN_ROUTE_UNDECLARED"},
		{"scope cannot bypass step up", http.MethodDelete, "/api/user/" + strconv.Itoa(owner.Id), userWrite, http.StatusForbidden, "SECURITY_PROOF_REQUIRED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer "+test.token)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, test.status, response.Code)
			if test.code != "" {
				assert.Contains(t, response.Body.String(), test.code)
			}
			if test.status == http.StatusOK {
				var body struct {
					Success bool `json:"success"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				assert.True(t, body.Success)
			}
		})
	}
	var stillPresent model.User
	require.NoError(t, db.First(&stillPresent, owner.Id).Error, "missing step-up must not delete the user")

	// Neither a stored grant nor its catalog eligibility replaces live RBAC.
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: false, authz.ActionInvoiceSettings: false}}))
	for _, test := range []struct{ path, token string }{{"/api/admin/invoices?page_size=101", reviewer}, {"/api/option/invoice", settings}} {
		response := performInvoiceRouteRequest(engine, admin.Id, test.token, http.MethodGet, test.path, "")
		assert.Equal(t, http.StatusForbidden, response.Code)
		assert.Contains(t, response.Body.String(), constant.InvoiceCodeForbidden)
		assertPrivateNoStore(t, response)
	}
	require.NoError(t, db.Model(root).Update("role", common.RoleAdminUser).Error)
	response := performInvoiceRouteRequest(engine, root.Id, evidenceRead, http.MethodGet, "/api/system-task/invoice-payment-evidence/backfill/invalid", "")
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_INSUFFICIENT_PRIVILEGE")

	// A valid PAT must not bypass CSRF protection on the separate cookie
	// refresh/logout surface. No refresh credential is used or validated here.
	previousSecure, previousOrigins := common.SessionCookieSecure, common.SessionCookieTrustedURLs
	common.SessionCookieSecure, common.SessionCookieTrustedURLs = true, nil
	t.Cleanup(func() { common.SessionCookieSecure, common.SessionCookieTrustedURLs = previousSecure, previousOrigins })
	for _, path := range []string{"/api/user/auth/refresh", "/api/user/auth/logout"} {
		for _, origin := range []string{"", "null", "https://other.example.test"} {
			request := httptest.NewRequest(http.MethodPost, "https://dashboard.example.test"+path, nil)
			request.Header.Set("Authorization", "Bearer "+walletRead)
			if origin != "" {
				request.Header.Set("Origin", origin)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, http.StatusForbidden, response.Code, path)
			assert.Contains(t, response.Body.String(), "AUTH_ORIGIN_FORBIDDEN")
		}
	}
}
