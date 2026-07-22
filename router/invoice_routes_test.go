package router

import (
	"net/http"
	"testing"

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
	}
	adminRoutes := map[string]string{
		http.MethodGet + " /invoices":               "AdminListInvoiceApplications",
		http.MethodGet + " /invoices/:id":           "AdminGetInvoiceApplication",
		http.MethodPost + " /invoices/:id/review":   "AdminReviewInvoiceApplication",
		http.MethodPost + " /invoices/:id/reject":   "AdminRejectInvoiceApplication",
		http.MethodPost + " /invoices/:id/document": "AdminUploadInvoiceDocument",
	}
	assertInvoiceRouteSet(t, invoiceUserRoutes, userRoutes)
	assertInvoiceRouteSet(t, invoiceAdminRoutes, adminRoutes)
}

func assertInvoiceRouteSet(t *testing.T, routes []invoiceRoute, expected map[string]string) {
	t.Helper()
	actual := make(map[string]string, len(routes))
	for _, route := range routes {
		actual[route.method+" "+route.path] = route.handlerName
	}
	assert.Equal(t, expected, actual)
}
