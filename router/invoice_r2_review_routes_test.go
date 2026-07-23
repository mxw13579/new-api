package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

func setupInvoiceReviewRouteContract(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	require.NoError(t, db.Exec("CREATE TABLE invoice_route_side_effects (id integer primary key)").Error)
	t.Cleanup(func() {
		common.IsMasterNode = previousMaster
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestInvoiceAdminRoutesUseIndependentReviewAndDocumentPermissions(t *testing.T) {
	expected := map[string]authz.Permission{
		http.MethodPost + " /invoices/:id/review":   authz.InvoiceReview,
		http.MethodPost + " /invoices/:id/reject":   authz.InvoiceReview,
		http.MethodPost + " /invoices/:id/document": authz.InvoiceDocumentUpload,
	}

	for _, route := range invoiceAdminRoutes {
		permission, ok := expected[route.method+" "+route.path]
		if !ok {
			continue
		}
		require.NotNil(t, route.permission)
		assert.Equal(t, permission, *route.permission)
		delete(expected, route.method+" "+route.path)
	}
	assert.Empty(t, expected)
}

func TestInvoiceAdminPermissionMatrixDeniesBeforeHandlerAndSideEffects(t *testing.T) {
	db := setupInvoiceReviewRouteContract(t)

	type subject struct {
		name      string
		userID    int
		role      int
		overrides authz.PermissionsMap
		reviewOK  bool
		uploadOK  bool
	}
	subjects := []subject{
		{name: "unauthenticated", userID: 0, role: common.RoleCommonUser},
		{name: "no invoice permission", userID: 101, role: common.RoleCommonUser},
		{name: "review only", userID: 102, role: common.RoleAdminUser, overrides: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: true, authz.ActionInvoiceDocumentUpload: false}}, reviewOK: true},
		{name: "upload only", userID: 103, role: common.RoleAdminUser, overrides: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: false, authz.ActionInvoiceDocumentUpload: true}}, uploadOK: true},
		{name: "admin explicitly denied review", userID: 104, role: common.RoleAdminUser, overrides: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: false}}, uploadOK: true},
		{name: "admin explicitly denied upload", userID: 105, role: common.RoleAdminUser, overrides: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceDocumentUpload: false}}, reviewOK: true},
		{name: "admin explicitly allowed both", userID: 106, role: common.RoleAdminUser, overrides: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: true, authz.ActionInvoiceDocumentUpload: true}}, reviewOK: true, uploadOK: true},
	}

	for _, subject := range subjects {
		if subject.overrides != nil {
			require.NoError(t, authz.SetUserPermissions(subject.userID, subject.overrides))
		}
		for _, endpoint := range []struct {
			name       string
			path       string
			permission *authz.Permission
			allowed    bool
		}{
			{name: "review", path: "/invoices/1/review", permission: &authz.InvoiceReview, allowed: subject.reviewOK},
			{name: "document upload", path: "/invoices/1/document", permission: &authz.InvoiceDocumentUpload, allowed: subject.uploadOK},
		} {
			t.Run(subject.name+"/"+endpoint.name, func(t *testing.T) {
				invocations := 0
				engine := gin.New()
				engine.Use(func(c *gin.Context) {
					c.Set("id", subject.userID)
					c.Set("role", subject.role)
				})
				registerInvoiceRoutes(engine.Group(""), []invoiceRoute{{
					method: http.MethodPost, path: endpoint.path, handlerName: "sideEffectSentinel", permission: endpoint.permission,
					handler: func(c *gin.Context) {
						invocations++
						require.NoError(t, db.Exec("INSERT INTO invoice_route_side_effects DEFAULT VALUES").Error)
						c.Status(http.StatusNoContent)
					},
				}})

				recorder := httptest.NewRecorder()
				engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, endpoint.path, nil))

				if endpoint.allowed {
					assert.Equal(t, http.StatusNoContent, recorder.Code)
					assert.Equal(t, 1, invocations)
					require.NoError(t, db.Exec("DELETE FROM invoice_route_side_effects").Error)
					return
				}
				assert.Equal(t, http.StatusForbidden, recorder.Code)
				assert.Zero(t, invocations)
				var payload struct {
					Data struct {
						Code string `json:"code"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
				assert.Equal(t, constant.InvoiceCodeForbidden, payload.Data.Code)
				var sideEffects int64
				require.NoError(t, db.Table("invoice_route_side_effects").Count(&sideEffects).Error)
				assert.Zero(t, sideEffects)
			})
		}
	}
}
