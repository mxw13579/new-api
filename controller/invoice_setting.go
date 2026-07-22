package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func GetInvoiceSetting(c *gin.Context) {
	writeInvoiceSuccess(c, http.StatusOK, operation_setting.GetInvoiceSetting())
}

func UpdateInvoiceSetting(c *gin.Context) {
	var setting operation_setting.InvoiceSetting
	if err := common.DecodeStrictJSONObject(c.Request.Body, &setting, false); err != nil || setting.Validate() != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	values := map[string]string{
		"invoice_setting.personal_enabled":        strconv.FormatBool(setting.PersonalEnabled),
		"invoice_setting.company_enabled":         strconv.FormatBool(setting.CompanyEnabled),
		"invoice_setting.application_window_days": strconv.Itoa(setting.ApplicationWindowDays),
		"invoice_setting.minimum_amount_minor":    strconv.FormatInt(setting.MinimumAmountMinor, 10),
		"invoice_setting.fee_quota":               strconv.FormatInt(setting.FeeQuota, 10),
		"invoice_setting.pdf_retention_days":      strconv.Itoa(setting.PDFRetentionDays),
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, operation_setting.GetInvoiceSetting())
}
