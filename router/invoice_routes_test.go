package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/service/authz"
	"github.com/stretchr/testify/assert"
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

func assertInvoiceRouteSet(t *testing.T, routes []invoiceRoute, expected map[string]string) {
	t.Helper()
	actual := make(map[string]string, len(routes))
	for _, route := range routes {
		actual[route.method+" "+route.path] = route.handlerName
	}
	assert.Equal(t, expected, actual)
}
