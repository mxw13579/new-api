package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPersonalInvoiceRouteContract(t *testing.T) {
	userRoutes := map[string]string{
		http.MethodGet + " /invoice/config":          "GetInvoiceConfig",
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
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.InvoiceApplication{}, &model.InvoiceDocument{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
	})

	accessToken := "invoice-download-local-test-identity"
	user := model.User{Username: "invoice-download-user", Password: "test-password", AccessToken: &accessToken,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "invoice-download-aff"}
	require.NoError(t, db.Create(&user).Error)
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
			{name: "invalid authentication", token: "invalid-local-token", wantStatus: http.StatusUnauthorized, wantCode: "AUTH_UNAUTHORIZED"},
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
				if test.wantStatus == http.StatusConflict {
					assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", response.Header().Get("Cache-Control"))
					assert.Equal(t, "no-cache", response.Header().Get("Pragma"))
					assert.Equal(t, "0", response.Header().Get("Expires"))
				}
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

func TestInvoiceDocumentProductionRouteMasksCrossOwnerLikeMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.InvoiceApplication{}, &model.InvoiceDocument{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
	})

	accessToken := "invoice-document-mask-test"
	user := model.User{Username: "invoice-document-user", Password: "test-password", AccessToken: &accessToken,
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "invoice-document-aff"}
	require.NoError(t, db.Create(&user).Error)
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
