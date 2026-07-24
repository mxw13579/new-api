package router

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

type invoiceRoute struct {
	method       string
	path         string
	handlerName  string
	handler      gin.HandlerFunc
	permission   *authz.Permission
	disableCache bool
}

var invoiceUserRoutes = []invoiceRoute{
	{http.MethodGet, "/invoice/config", "GetInvoiceConfig", controller.GetInvoiceConfig, nil, true},
	{http.MethodGet, "/invoice/profiles", "ListInvoiceProfiles", controller.ListInvoiceProfiles, nil, true},
	{http.MethodPost, "/invoice/profiles", "CreateInvoiceProfile", controller.CreateInvoiceProfile, nil, true},
	{http.MethodPut, "/invoice/profiles", "UpdateInvoiceProfile", controller.UpdateInvoiceProfile, nil, true},
	{http.MethodDelete, "/invoice/profiles", "DeleteInvoiceProfile", controller.DeleteInvoiceProfile, nil, true},
	{http.MethodGet, "/invoice/eligible-orders", "ListEligibleInvoiceOrders", controller.ListEligibleInvoiceOrders, nil, true},
	{http.MethodPost, "/invoices", "CreateInvoiceApplication", controller.CreateInvoiceApplication, nil, true},
	{http.MethodGet, "/invoices", "ListInvoiceApplications", controller.ListInvoiceApplications, nil, true},
	{http.MethodGet, "/invoices/:id", "GetInvoiceApplication", controller.GetInvoiceApplication, nil, true},
	{http.MethodGet, "/invoices/:id/document", "DownloadInvoiceDocument", controller.DownloadInvoiceDocument, nil, true},
	{http.MethodPost, "/invoices/:id/cancel", "CancelInvoiceApplication", controller.CancelInvoiceApplication, nil, true},
}

var invoiceAdminRoutes = []invoiceRoute{
	{http.MethodGet, "/invoices", "AdminListInvoiceApplications", controller.AdminListInvoiceApplications, &authz.InvoiceReview, true},
	{http.MethodGet, "/invoices/:id", "AdminGetInvoiceApplication", controller.AdminGetInvoiceApplication, &authz.InvoiceReview, true},
	{http.MethodPost, "/invoices/:id/review", "AdminReviewInvoiceApplication", controller.AdminReviewInvoiceApplication, &authz.InvoiceReview, true},
	{http.MethodPost, "/invoices/:id/reject", "AdminRejectInvoiceApplication", controller.AdminRejectInvoiceApplication, &authz.InvoiceReview, true},
	{http.MethodPost, "/invoices/:id/document", "AdminUploadInvoiceDocument", controller.AdminUploadInvoiceDocument, &authz.InvoiceDocumentUpload, true},
}

var invoiceOptionRoutes = []invoiceRoute{
	{http.MethodGet, "/invoice", "GetInvoiceSetting", controller.GetInvoiceSetting, &authz.InvoiceSettings, true},
	{http.MethodPut, "/invoice", "UpdateInvoiceSetting", controller.UpdateInvoiceSetting, &authz.InvoiceSettings, true},
}

func registerInvoiceRoutes(group *gin.RouterGroup, routes []invoiceRoute) {
	for _, route := range routes {
		handlers := make([]gin.HandlerFunc, 0, 2)
		if route.disableCache {
			handlers = append(handlers, middleware.DisableCache())
		}
		if route.permission != nil {
			permission := *route.permission
			handlers = append(handlers, func(c *gin.Context) {
				if authz.Can(c.GetInt("id"), c.GetInt("role"), permission) {
					c.Next()
					return
				}
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"success": false, "message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
					"data": gin.H{"code": constant.InvoiceCodeForbidden},
				})
			})
		}
		handlers = append(handlers, route.handler)
		group.Handle(route.method, route.path, handlers...)
	}
}
