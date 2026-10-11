package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPersonalInvoiceRouteContract(t *testing.T) {
	userRoutes := map[string]string{
		http.MethodGet + " /invoice/config":          "GetInvoiceConfig",
		http.MethodGet + " /invoice/fee-ledger":      "ListInvoiceFeeHistory",
		http.MethodGet + " /invoice/profiles":        "ListInvoiceProfiles",
		http.MethodPost + " /invoice/profiles":       "CreateInvoiceProfile",
		http.MethodPut + " /invoice/profiles":        "UpdateInvoiceProfile",
		http.MethodDelete + " /invoice/profiles":     "DeleteInvoiceProfile",
		http.MethodGet + " /invoice/eligible-orders": "ListEligibleInvoiceOrders",
		http.MethodPost + " /invoices":               "CreateInvoiceApplication",
		http.MethodGet + " /invoices":                "ListInvoiceApplications",
		http.MethodGet + " /invoices/:id":            "GetInvoiceApplication",
		http.MethodPost + " /invoices/:id/cancel":    "CancelInvoiceApplication",
		http.MethodGet + " /invoices/:id/document":   "DownloadInvoiceDocument",
	}
	adminRoutes := map[string]string{
		http.MethodGet + " /invoice/fee-ledger":     "AdminListInvoiceFeeHistory",
		http.MethodGet + " /invoices":               "AdminListInvoiceApplications",
		http.MethodGet + " /invoices/:id":           "AdminGetInvoiceApplication",
		http.MethodPost + " /invoices/:id/review":   "AdminReviewInvoiceApplication",
		http.MethodPost + " /invoices/:id/reject":   "AdminRejectInvoiceApplication",
		http.MethodPost + " /invoices/:id/document": "AdminUploadInvoiceDocument",
	}
	optionRoutes := map[string]string{
		http.MethodGet + " /invoice": "GetInvoiceSetting",
		http.MethodPut + " /invoice": "UpdateInvoiceSetting",
	}
	assertInvoiceRouteSet(t, invoiceUserRoutes, userRoutes)
	assertInvoiceRouteSet(t, invoiceAdminRoutes, adminRoutes)
	assertInvoiceRouteSet(t, invoiceOptionRoutes, optionRoutes)
}

func TestInvoiceAdminFeeLedgerUsesReviewPermissionAndOwnerRouteRemainsSelfScoped(t *testing.T) {
	var owner, admin *invoiceRoute
	for i := range invoiceUserRoutes {
		if invoiceUserRoutes[i].path == "/invoice/fee-ledger" {
			owner = &invoiceUserRoutes[i]
		}
	}
	for i := range invoiceAdminRoutes {
		if invoiceAdminRoutes[i].path == "/invoice/fee-ledger" {
			admin = &invoiceAdminRoutes[i]
		}
	}
	require.NotNil(t, owner)
	assert.Nil(t, owner.permission)
	require.NotNil(t, admin)
	require.NotNil(t, admin.permission)
	assert.Equal(t, authz.InvoiceReview, *admin.permission)
	assert.NotEqual(t, owner.handlerName, admin.handlerName)
}

func TestInvoiceSettingRoutesUseInvoicePermissionForAdminAndRoot(t *testing.T) {
	matched := 0
	for _, route := range invoiceOptionRoutes {
		matched++
		assert.Equal(t, &authz.InvoiceSettings, route.permission)
		assert.Contains(t, authz.PermissionsForRole(authz.BuiltInRoleAdmin), authz.InvoiceSettings)
		rootGrants := authz.Roles()[0].Grants
		assert.True(t, rootGrants[authz.ResourceInvoice][authz.ActionInvoiceSettings])
	}
	assert.Equal(t, 2, matched)
}

func TestInvoiceDownloadProductionRouteRequiresUserAuthAndUsesNoStore(t *testing.T) {
	db := setupAccessTokenRouteDB(t)
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}, &model.InvoiceDocument{}))
	user := model.User{Username: "invoice-download-user", AuthVersion: 1,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "invoice-download-aff"}
	require.NoError(t, db.Create(&user).Error)
	accessToken := invoiceRouteSessionToken(t, user.Id)
	application := model.InvoiceApplication{
		ApplicationNo: "INV-ROUTE-DOWNLOAD", UserID: user.Id, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusIssued,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, db.Create(&application).Error)

	engine := gin.New()
	SetApiRouter(engine)
	for _, path := range []string{fmt.Sprintf("/api/user/invoices/%d/document", application.ID)} {
		for _, test := range []struct {
			name       string
			token      string
			wantStatus int
			wantCode   string
		}{
			{name: "missing authentication", wantStatus: http.StatusUnauthorized, wantCode: "AUTH_UNAUTHORIZED"},
			{name: "invalid authentication", token: model.AccessTokenPrefix + "invalid-local-token", wantStatus: http.StatusUnauthorized, wantCode: "AUTH_UNAUTHORIZED"},
			{name: "authenticated owned unavailable", token: accessToken, wantStatus: http.StatusConflict, wantCode: constant.InvoiceCodeDocumentUnavailable},
		} {
			t.Run(test.name+" "+path, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				if test.token != "" {
					request.Header.Set("Authorization", "Bearer "+test.token)
				}
				response := httptest.NewRecorder()

				engine.ServeHTTP(response, request)

				assert.Equal(t, test.wantStatus, response.Code)
				assert.Contains(t, response.Body.String(), test.wantCode)
				assert.Empty(t, response.Header().Get("Location"))
				assert.NotContains(t, response.Body.String(), "INVOICE_R2_")
				assertPrivateNoStore(t, response)
			})
		}
	}

	for _, test := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCache  bool
	}{
		{name: "config success", method: http.MethodGet, path: "/api/user/invoice/config", wantStatus: http.StatusOK, wantCache: true},
		{name: "missing document", method: http.MethodGet, path: "/api/user/invoices/999999/document", wantStatus: http.StatusNotFound, wantCache: true},
		{name: "retired document URL", method: http.MethodPost, path: fmt.Sprintf("/api/user/invoices/%d/document-url", application.ID), wantStatus: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+accessToken)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)

			assert.Equal(t, test.wantStatus, response.Code)
			if test.wantCache {
				assertPrivateNoStore(t, response)
			}
		})
	}
}

func TestEveryInvoiceRouteUsesPrivateNoStore(t *testing.T) {
	for _, routes := range [][]invoiceRoute{invoiceUserRoutes, invoiceAdminRoutes, invoiceOptionRoutes} {
		for _, route := range routes {
			assert.True(t, route.disableCache, route.method+" "+route.path)
		}
	}
}

func TestInvoiceFeeLedgerRoutesEnforceScopePermissionIdentityAndNoStore(t *testing.T) {
	db := setupAccessTokenRouteDB(t)
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}, &model.InvoiceFeeLedgerEntry{}, &model.Log{}))
	user := model.User{Username: "fee-ledger-owner", AuthVersion: 1,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "fee-ledger-owner"}
	require.NoError(t, db.Create(&user).Error)
	token := createRouteScopedToken(t, user.Id, user.Role, "wallet:read")
	application := model.InvoiceApplication{ApplicationNo: "INV-FEE-OWNER", UserID: user.Id, RequestID: "fee-owner", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusSubmitted, PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		Currency: constant.InvoiceCurrencyCNY, FeeMethod: "wallet_quota", FeeStatus: constant.InvoiceFeeStatusPaid,
		ProfileSnapshot: `{}`, PolicySnapshot: `{"fee_percent":5}`, SubmittedAt: 1}
	require.NoError(t, db.Create(&application).Error)
	require.NoError(t, db.Create(&model.InvoiceFeeLedgerEntry{ApplicationID: application.ID, UserID: user.Id,
		EntryType: model.InvoiceFeeEntryTypeCharge, Quota: 5, IdempotencyKey: "fee-owner", Status: model.InvoiceFeeEntryStatusPending}).Error)
	other := model.User{Username: "fee-ledger-other", DisplayName: "Other Owner", AuthVersion: 1,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "fee-ledger-other"}
	require.NoError(t, db.Create(&other).Error)
	otherApplication := model.InvoiceApplication{ApplicationNo: "INV-FEE-OTHER", UserID: other.Id, RequestID: "fee-other", RequestFingerprint: "other-fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusSubmitted, PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		Currency: constant.InvoiceCurrencyCNY, FeeMethod: "wallet_quota", FeeStatus: constant.InvoiceFeeStatusPaid,
		ProfileSnapshot: `{}`, PolicySnapshot: `{"fee_percent":7}`, SubmittedAt: 2}
	require.NoError(t, db.Create(&otherApplication).Error)
	require.NoError(t, db.Create(&model.InvoiceFeeLedgerEntry{ApplicationID: otherApplication.ID, UserID: other.Id,
		EntryType: model.InvoiceFeeEntryTypeCharge, Quota: 7, IdempotencyKey: "fee-other", Status: model.InvoiceFeeEntryStatusApplied}).Error)
	admin := model.User{Username: "fee-ledger-reviewer", AuthVersion: 1,
		Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "fee-ledger-reviewer"}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{
		authz.ResourceInvoice: {authz.ActionInvoiceReview: true},
	}))
	adminToken := createRouteScopedToken(t, admin.Id, admin.Role, service.AccessTokenScopeOf(authz.InvoiceReview))
	engine := gin.New()
	SetApiRouter(engine)

	unauthenticated := httptest.NewRecorder()
	engine.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/user/invoice/fee-ledger", nil))
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code)
	assertPrivateNoStore(t, unauthenticated)

	request := httptest.NewRequest(http.MethodGet, "/api/user/invoice/fee-ledger", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	authenticated := httptest.NewRecorder()
	engine.ServeHTTP(authenticated, request)
	assert.Equal(t, http.StatusOK, authenticated.Code)
	assert.Contains(t, authenticated.Body.String(), "INV-FEE-OWNER")
	assert.NotContains(t, authenticated.Body.String(), "INV-FEE-OTHER")
	assert.NotContains(t, authenticated.Body.String(), "user_id")
	assertPrivateNoStore(t, authenticated)

	adminResponse := performInvoiceRouteRequest(engine, admin.Id, adminToken, http.MethodGet, "/api/admin/invoice/fee-ledger", "")
	assert.Equal(t, http.StatusOK, adminResponse.Code)
	assert.Contains(t, adminResponse.Body.String(), "INV-FEE-OWNER")
	assert.Contains(t, adminResponse.Body.String(), "INV-FEE-OTHER")
	assert.Contains(t, adminResponse.Body.String(), `"user_id":`)
	assert.Contains(t, adminResponse.Body.String(), "Other Owner")
	assertPrivateNoStore(t, adminResponse)
}

func TestInvoiceDocumentProductionRouteMasksCrossOwnerLikeMissing(t *testing.T) {
	db := setupAccessTokenRouteDB(t)
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}, &model.InvoiceDocument{}))
	user := model.User{Username: "invoice-document-user", AuthVersion: 1,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "invoice-document-aff"}
	require.NoError(t, db.Create(&user).Error)
	accessToken := invoiceRouteSessionToken(t, user.Id)
	otherApplication := model.InvoiceApplication{
		ApplicationNo: "INV-OTHER-OWNER", UserID: user.Id + 100, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusIssued,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, db.Create(&otherApplication).Error)

	engine := gin.New()
	SetApiRouter(engine)
	responses := make([]*httptest.ResponseRecorder, 0, 2)
	for _, id := range []int64{otherApplication.ID, otherApplication.ID + 999} {
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/user/invoices/%d/document", id), nil)
		request.Header.Set("Authorization", "Bearer "+accessToken)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotFound, response.Code)
		assertPrivateNoStore(t, response)
		assert.Empty(t, response.Header().Get("Location"))
		assert.NotContains(t, response.Body.String(), "download_url")
		responses = append(responses, response)
	}
	assert.Equal(t, responses[0].Header(), responses[1].Header())
	assert.Equal(t, responses[0].Body.String(), responses[1].Body.String())
}

func TestInvoiceRouteNoStoreMiddlewarePrecedesPermissionAndHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		permission *authz.Permission
		handler    gin.HandlerFunc
		wantStatus int
	}{
		{
			name:       "authenticated permission denial",
			permission: &authz.InvoiceReview,
			handler: func(c *gin.Context) {
				c.Status(http.StatusOK)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "handler not found",
			handler: func(c *gin.Context) {
				c.Status(http.StatusNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "handler success",
			handler: func(c *gin.Context) {
				c.Status(http.StatusOK)
			},
			wantStatus: http.StatusOK,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			group := engine.Group("/api")
			registerInvoiceRoutes(group, []invoiceRoute{{
				method: http.MethodGet, path: "/invoice", handlerName: "test",
				handler: test.handler, permission: test.permission, disableCache: true,
			}})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/invoice", nil))

			assert.Equal(t, test.wantStatus, response.Code)
			assertPrivateNoStore(t, response)
		})
	}
}

func TestInvoiceDownloadRouteNoStoreHeadersSurviveRedirect(t *testing.T) {
	engine := gin.New()
	group := engine.Group("/api/user")
	registerInvoiceRoutes(group, []invoiceRoute{{
		method: http.MethodGet, path: "/invoices/:id/document", handlerName: "DownloadInvoiceDocument",
		handler: func(c *gin.Context) {
			c.Redirect(http.StatusFound, "https://private.example.test/invoice.pdf?signature=test-only")
		},
		disableCache: true,
	}})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/user/invoices/7/document", nil))

	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, "https://private.example.test/invoice.pdf?signature=test-only", response.Header().Get("Location"))
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", response.Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache", response.Header().Get("Pragma"))
	assert.Equal(t, "0", response.Header().Get("Expires"))
}

func assertPrivateNoStore(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", response.Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache", response.Header().Get("Pragma"))
	assert.Equal(t, "0", response.Header().Get("Expires"))
}

func assertInvoiceRouteSet(t *testing.T, routes []invoiceRoute, expected map[string]string) {
	t.Helper()
	actual := make(map[string]string, len(routes))
	for _, route := range routes {
		actual[route.method+" "+route.path] = route.handlerName
	}
	assert.Equal(t, expected, actual)
}

func setupInvoiceRouteSecurityFixture(t *testing.T) (*gin.Engine, *gorm.DB, string, int) {
	t.Helper()
	db := setupAccessTokenRouteDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.InvoiceProfile{},
		&model.InvoiceApplication{}, &model.InvoiceItem{}, &model.InvoiceFeeLedgerEntry{},
		&model.InvoiceIssuance{}, &model.InvoiceDocument{},
	))
	const userID = 8101
	require.NoError(t, db.Create(&model.User{
		Id: userID, Username: "invoice-route-security", AuthVersion: 1,
		Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "invoice-route-security",
	}).Error)
	token := invoiceRouteSessionToken(t, userID)
	engine := gin.New()
	SetApiRouter(engine)
	return engine, db, token, userID
}

func performInvoiceRouteRequest(engine *gin.Engine, userID int, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("New-Api-User", fmt.Sprint(userID))
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func TestInvoicePDFRoutesRequireLiveBrowserSessions(t *testing.T) {
	engine, db, session, userID := setupInvoiceRouteSecurityFixture(t)
	legacy := "synthetic-legacy-invoice-token"
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("access_token", legacy).Error)
	previousDeadline := model.LegacyAccessTokenRetireAt()
	require.NoError(t, model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()))
	t.Cleanup(func() {
		// Zero and a past deadline both mean retired. The production API only
		// accepts positive persisted deadlines.
		require.NoError(t, db.Model(&model.Option{}).Where("key = ?", "LegacyAccessTokenRetireAt").Update("value", fmt.Sprint(max(previousDeadline, 1))).Error)
		require.NoError(t, model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()))
	})
	var scopes []string
	for _, group := range service.AccessTokenCatalog(userID, common.RoleAdminUser) {
		for _, resource := range group.Resources {
			for _, action := range resource.Actions {
				scopes = append(scopes, resource.Resource+":"+action.Action)
			}
		}
	}
	scoped := createRouteScopedToken(t, userID, common.RoleAdminUser, scopes...)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/user/invoices/invalid/document"},
		{http.MethodPost, "/api/admin/invoices/invalid/document"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			for _, credential := range []string{scoped, legacy} {
				response := performInvoiceRouteRequest(engine, userID, credential, route.method, route.path, "{}")
				assert.Equal(t, http.StatusForbidden, response.Code)
				assert.Contains(t, response.Body.String(), "AUTH_SESSION_REQUIRED")
				assert.Empty(t, response.Header().Get("Location"))
				assertPrivateNoStore(t, response)
			}
			response := performInvoiceRouteRequest(engine, userID, session, route.method, route.path, "{}")
			assert.Equal(t, http.StatusBadRequest, response.Code, "live session reaches ID validation, not object storage")
			assert.Contains(t, response.Body.String(), constant.InvoiceCodeInvalidRequest)
			assertPrivateNoStore(t, response)
		})
	}
	require.NoError(t, authz.SetUserPermissions(userID, authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceDocumentUpload: false}}))
	denied := performInvoiceRouteRequest(engine, userID, session, http.MethodPost, "/api/admin/invoices/invalid/document", "{}")
	assert.Equal(t, http.StatusForbidden, denied.Code)
	assert.Contains(t, denied.Body.String(), constant.InvoiceCodeForbidden)
	assertPrivateNoStore(t, denied)

	identity, internal, err := service.ParseDashboardAccessToken(session)
	require.NoError(t, err)
	require.True(t, internal)
	_, err = model.RevokeUserSession(userID, identity.SessionID, "synthetic-route-test")
	require.NoError(t, err)
	revoked := performInvoiceRouteRequest(engine, userID, session, http.MethodGet, "/api/user/invoices/invalid/document", "")
	assert.Equal(t, http.StatusUnauthorized, revoked.Code)
	assert.Contains(t, revoked.Body.String(), "AUTH_SESSION_REVOKED")
	assertPrivateNoStore(t, revoked)
}

func TestInvoiceMetadataIntersectsTokenGrantWithLivePermissions(t *testing.T) {
	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		t.Run(fmt.Sprint(role), func(t *testing.T) {
			engine, db, session, userID := setupInvoiceRouteSecurityFixture(t)
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("role", role).Error)
			require.NoError(t, authz.SetUserPermissions(userID, authz.PermissionsMap{authz.ResourceInvoice: {
				authz.ActionInvoiceReview: true, authz.ActionInvoiceSensitiveRead: true,
			}}))
			reviewScope := service.AccessTokenScopeOf(authz.InvoiceReview)
			sensitiveScope := service.AccessTokenScopeOf(authz.InvoiceSensitiveRead)
			review := createRouteScopedToken(t, userID, role, reviewScope)
			both := createRouteScopedToken(t, userID, role, reviewScope, sensitiveScope)
			sensitive := createRouteScopedToken(t, userID, role, sensitiveScope)
			unrelated := createRouteScopedToken(t, userID, role, "wallet:read")
			applicationSequence := 0
			for _, test := range []struct {
				name, token string
				sensitive   bool
				forbidden   bool
				deny        *authz.Permission
			}{
				{name: "review masks despite account permission", token: review},
				{name: "both grants disclose", token: both, sensitive: true},
				{name: "sensitive grant is not review", token: sensitive, forbidden: true},
				{name: "wallet grant is not review", token: unrelated, forbidden: true},
				{name: "browser retains account permissions", token: session, sensitive: true},
				{name: "revoked sensitive permission masks stored grant", token: both, deny: &authz.InvoiceSensitiveRead},
				{name: "revoked review permission blocks stored grant", token: both, deny: &authz.InvoiceReview, forbidden: true},
			} {
				if role == common.RoleRootUser && test.deny != nil {
					continue // Root's live permissions are unconditional; its PAT grant is not.
				}
				t.Run(test.name, func(t *testing.T) {
					permissions := authz.PermissionsMap{authz.ResourceInvoice: {
						authz.ActionInvoiceReview: true, authz.ActionInvoiceSensitiveRead: true,
					}}
					if test.deny != nil {
						permissions[test.deny.Resource][test.deny.Action] = false
					}
					require.NoError(t, authz.SetUserPermissions(userID, permissions))
					for _, route := range []struct{ method, suffix, body, status string }{
						{http.MethodGet, "", "", constant.InvoiceApplicationStatusSubmitted},
						{http.MethodPost, "/review", `{"action":"reviewing","expected_status":"submitted"}`, constant.InvoiceApplicationStatusSubmitted},
						// A legitimate retry of a terminal rejection must not disclose
						// more than a first response. No payment/refund is performed.
						{http.MethodPost, "/reject", `{"expected_status":"submitted","reason":"synthetic rejection"}`, constant.InvoiceApplicationStatusRejected},
					} {
						applicationSequence++
						application := model.InvoiceApplication{
							ApplicationNo: fmt.Sprintf("INV-GRANT-%d", applicationSequence), UserID: userID,
							RequestID: fmt.Sprintf("grant-%d", applicationSequence), RequestFingerprint: "synthetic-fingerprint",
							Type: constant.InvoiceTypeCompany, Status: route.status,
							PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
							FeeMethod: model.InvoiceFeeMethodWalletQuota, FeeStatus: constant.InvoiceFeeStatusNotRequired,
							ProfileSnapshot: `{"title":"Synthetic Company","tax_number":"SYNTHETIC-TAX","identity_card_number":"SYNTHETIC-ID"}`,
							PolicySnapshot:  `{}`, SubmittedAt: 1,
						}
						require.NoError(t, db.Create(&application).Error)
						path := fmt.Sprintf("/api/admin/invoices/%d%s", application.ID, route.suffix)
						response := performInvoiceRouteRequest(engine, userID, test.token, route.method, path, route.body)
						assertPrivateNoStore(t, response)
						if test.forbidden {
							assert.Equal(t, http.StatusForbidden, response.Code)
							code := "ACCESS_TOKEN_SCOPE_DENIED"
							if test.deny != nil {
								code = constant.InvoiceCodeForbidden
							}
							assert.Contains(t, response.Body.String(), code)
							var unchanged model.InvoiceApplication
							require.NoError(t, db.First(&unchanged, application.ID).Error)
							assert.Equal(t, application, unchanged)
							continue
						}
						require.Equal(t, http.StatusOK, response.Code)
						var payload struct {
							Success bool                         `json:"success"`
							Data    dto.InvoiceApplicationDetail `json:"data"`
						}
						require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
						require.True(t, payload.Success)
						if test.sensitive {
							assert.Equal(t, "Synthetic Company", payload.Data.ProfileSnapshot.Title)
							assert.Equal(t, "SYNTHETIC-TAX", payload.Data.ProfileSnapshot.TaxNumber)
							assert.Equal(t, "SYNTHETIC-ID", payload.Data.ProfileSnapshot.IdentityCardNumber)
						} else {
							assert.Empty(t, payload.Data.ProfileSnapshot.Title)
							assert.Empty(t, payload.Data.ProfileSnapshot.TaxNumber)
							assert.Empty(t, payload.Data.ProfileSnapshot.IdentityCardNumber)
						}
					}
				})
			}
		})
	}
}

func TestEveryProductionInvoiceRouteRejectsUnauthenticatedRequests(t *testing.T) {
	engine := gin.New()
	SetApiRouter(engine)
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/user/invoice/config"},
		{http.MethodGet, "/api/user/invoice/fee-ledger"},
		{http.MethodGet, "/api/user/invoice/profiles"},
		{http.MethodPost, "/api/user/invoice/profiles"},
		{http.MethodPut, "/api/user/invoice/profiles"},
		{http.MethodDelete, "/api/user/invoice/profiles"},
		{http.MethodGet, "/api/user/invoice/eligible-orders"},
		{http.MethodPost, "/api/user/invoices"},
		{http.MethodGet, "/api/user/invoices"},
		{http.MethodGet, "/api/user/invoices/1"},
		{http.MethodGet, "/api/user/invoices/1/document"},
		{http.MethodPost, "/api/user/invoices/1/cancel"},
		{http.MethodGet, "/api/admin/invoices"},
		{http.MethodGet, "/api/admin/invoice/fee-ledger"},
		{http.MethodGet, "/api/admin/invoices/1"},
		{http.MethodPost, "/api/admin/invoices/1/review"},
		{http.MethodPost, "/api/admin/invoices/1/reject"},
		{http.MethodPost, "/api/admin/invoices/1/document"},
		{http.MethodGet, "/api/option/invoice"},
		{http.MethodPut, "/api/option/invoice"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := performInvoiceRouteRequest(engine, 0, "", test.method, test.path, `{}`)
			assert.Equal(t, http.StatusUnauthorized, response.Code)
			assert.Contains(t, response.Body.String(), "AUTH_UNAUTHORIZED")
			assertPrivateNoStore(t, response)
		})
	}
}

func TestProductionInvoicePermissionRoutesRejectAuthenticatedAdminWithoutInvoicePermission(t *testing.T) {
	engine, _, token, userID := setupInvoiceRouteSecurityFixture(t)
	require.NoError(t, authz.SetUserPermissions(userID, authz.PermissionsMap{
		authz.ResourceInvoice: {
			authz.ActionInvoiceReview: false, authz.ActionInvoiceDocumentUpload: false, authz.ActionInvoiceSettings: false,
		},
	}))
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/invoices"},
		{http.MethodGet, "/api/admin/invoice/fee-ledger"},
		{http.MethodGet, "/api/admin/invoices/1"},
		{http.MethodPost, "/api/admin/invoices/1/review"},
		{http.MethodPost, "/api/admin/invoices/1/reject"},
		{http.MethodPost, "/api/admin/invoices/1/document"},
		{http.MethodGet, "/api/option/invoice"},
		{http.MethodPut, "/api/option/invoice"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := performInvoiceRouteRequest(engine, userID, token, test.method, test.path, `{}`)
			assert.Equal(t, http.StatusForbidden, response.Code)
			assert.Contains(t, response.Body.String(), constant.InvoiceCodeForbidden)
			assertPrivateNoStore(t, response)
		})
	}
}

func TestProductionInvoiceOwnerRoutesMaskCrossOwnerLikeMissing(t *testing.T) {
	engine, db, token, userID := setupInvoiceRouteSecurityFixture(t)
	otherProfile := model.InvoiceProfile{
		UserID: userID + 1, Type: constant.InvoiceTypePersonal, Title: "other owner", Version: 1,
	}
	require.NoError(t, db.Create(&otherProfile).Error)
	otherApplication := model.InvoiceApplication{
		UserID: userID + 1, ApplicationNo: "INV-CROSS-OWNER", RequestID: "cross-owner", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusSubmitted,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, db.Create(&otherApplication).Error)

	tests := []struct {
		name        string
		method      string
		crossPath   string
		missingPath string
		crossBody   string
		missingBody string
	}{
		{
			name: "update profile", method: http.MethodPut, crossPath: "/api/user/invoice/profiles", missingPath: "/api/user/invoice/profiles",
			crossBody:   fmt.Sprintf(`{"id":%d,"expected_version":1,"title":"masked","tax_number":"","is_default":false}`, otherProfile.ID),
			missingBody: `{"id":999999,"expected_version":1,"title":"masked","tax_number":"","is_default":false}`,
		},
		{
			name: "delete profile", method: http.MethodDelete, crossPath: "/api/user/invoice/profiles", missingPath: "/api/user/invoice/profiles",
			crossBody: fmt.Sprintf(`{"id":%d,"expected_version":1}`, otherProfile.ID), missingBody: `{"id":999999,"expected_version":1}`,
		},
		{name: "get application", method: http.MethodGet, crossPath: fmt.Sprintf("/api/user/invoices/%d", otherApplication.ID), missingPath: "/api/user/invoices/999999"},
		{name: "cancel application", method: http.MethodPost, crossPath: fmt.Sprintf("/api/user/invoices/%d/cancel", otherApplication.ID), missingPath: "/api/user/invoices/999999/cancel"},
		{name: "download document", method: http.MethodGet, crossPath: fmt.Sprintf("/api/user/invoices/%d/document", otherApplication.ID), missingPath: "/api/user/invoices/999999/document"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cross := performInvoiceRouteRequest(engine, userID, token, test.method, test.crossPath, test.crossBody)
			missing := performInvoiceRouteRequest(engine, userID, token, test.method, test.missingPath, test.missingBody)
			assert.Equal(t, http.StatusNotFound, cross.Code)
			assert.Equal(t, missing.Code, cross.Code)
			assert.Equal(t, missing.Body.String(), cross.Body.String())
			assert.NotContains(t, cross.Body.String(), otherApplication.ApplicationNo)
			assert.NotContains(t, cross.Body.String(), otherProfile.Title)
		})
	}
}
