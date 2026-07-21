package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoicePaymentEvidenceRouteMiddlewareContract(t *testing.T) {
	expected := map[string]struct {
		permission        authz.Permission
		criticalRateLimit bool
		disableCache      bool
	}{
		"POST /invoice-payment-evidence/backfill/preview":       {authz.InvoicePaymentEvidenceOperate, true, true},
		"POST /invoice-payment-evidence/backfill/:run_id/apply": {authz.InvoicePaymentEvidenceOperate, true, true},
		"GET /invoice-payment-evidence/backfill/:run_id":        {authz.InvoicePaymentEvidenceRead, false, true},
		"POST /invoice-payment-evidence/backfill/:run_id/stop":  {authz.InvoicePaymentEvidenceOperate, true, true},
	}
	require.Len(t, invoicePaymentEvidenceRoutes, len(expected))
	for _, route := range invoicePaymentEvidenceRoutes {
		contract, ok := expected[route.method+" "+route.path]
		require.True(t, ok, route.method+" "+route.path)
		assert.Equal(t, contract.permission, route.permission)
		assert.Equal(t, contract.criticalRateLimit, route.criticalRateLimit)
		assert.Equal(t, contract.disableCache, route.disableCache)
	}
}

func TestInvoicePaymentEvidenceRootRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	routes := map[string]string{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = route.Handler
	}

	expected := map[string]string{
		"POST /api/system-task/invoice-payment-evidence/backfill/preview":       "github.com/QuantumNous/new-api/controller.PreviewInvoicePaymentEvidenceBackfill",
		"POST /api/system-task/invoice-payment-evidence/backfill/:run_id/apply": "github.com/QuantumNous/new-api/controller.ApplyInvoicePaymentEvidenceBackfill",
		"GET /api/system-task/invoice-payment-evidence/backfill/:run_id":        "github.com/QuantumNous/new-api/controller.GetInvoicePaymentEvidenceBackfill",
		"POST /api/system-task/invoice-payment-evidence/backfill/:run_id/stop":  "github.com/QuantumNous/new-api/controller.StopInvoicePaymentEvidenceBackfill",
	}
	for route, handler := range expected {
		assert.Equal(t, handler, routes[route], route)
	}
}

func TestInvoicePaymentEvidenceRoutesRequireRootAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/system-task/invoice-payment-evidence/backfill/preview", `{}`},
		{http.MethodPost, "/api/system-task/invoice-payment-evidence/backfill/1/apply", `{}`},
		{http.MethodGet, "/api/system-task/invoice-payment-evidence/backfill/1", ""},
		{http.MethodPost, "/api/system-task/invoice-payment-evidence/backfill/1/stop", `{}`},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		engine.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, test.method+" "+test.path)
	}
}
