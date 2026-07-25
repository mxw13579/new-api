package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type invoiceSettingRequest struct {
	PersonalEnabled       bool   `json:"personal_enabled"`
	CompanyEnabled        bool   `json:"company_enabled"`
	ApplicationWindowDays int    `json:"application_window_days"`
	MinimumAmountMinor    int64  `json:"minimum_amount_minor"`
	FeePercent            int    `json:"fee_percent"`
	PDFRetentionDays      int    `json:"pdf_retention_days"`
	R2Endpoint            string `json:"r2_endpoint"`
	R2Bucket              string `json:"r2_bucket"`
	R2AccessKeyID         string `json:"r2_access_key_id"`
	R2SecretAccessKey     string `json:"r2_secret_access_key"`
}

type invoiceSettingResponse struct {
	PersonalEnabled       bool   `json:"personal_enabled"`
	CompanyEnabled        bool   `json:"company_enabled"`
	ApplicationWindowDays int    `json:"application_window_days"`
	MinimumAmountMinor    int64  `json:"minimum_amount_minor"`
	FeePercent            int    `json:"fee_percent"`
	PDFRetentionDays      int    `json:"pdf_retention_days"`
	R2Endpoint            string `json:"r2_endpoint"`
	R2Bucket              string `json:"r2_bucket"`
	R2AccessKeyID         string `json:"r2_access_key_id"`
	R2SecretConfigured    bool   `json:"r2_secret_configured"`
}

func safeInvoiceSetting(setting operation_setting.InvoiceSetting) invoiceSettingResponse {
	return invoiceSettingResponse{
		PersonalEnabled: setting.PersonalEnabled, CompanyEnabled: setting.CompanyEnabled,
		ApplicationWindowDays: setting.ApplicationWindowDays, MinimumAmountMinor: setting.MinimumAmountMinor,
		FeePercent: setting.FeePercent, PDFRetentionDays: setting.PDFRetentionDays,
		R2Endpoint: setting.R2Endpoint, R2Bucket: setting.R2Bucket, R2AccessKeyID: setting.R2AccessKeyID,
		R2SecretConfigured: strings.TrimSpace(setting.R2Secret) != "",
	}
}

// GetInvoiceSetting returns the mutable invoice policy to authorized option administrators.
func GetInvoiceSetting(c *gin.Context) {
	writeInvoiceSuccess(c, http.StatusOK, safeInvoiceSetting(operation_setting.GetInvoiceSetting()))
}

// UpdateInvoiceSetting validates and atomically persists the complete invoice policy.
func UpdateInvoiceSetting(c *gin.Context) {
	var request invoiceSettingRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	err := model.UpdateInvoiceSettingOptions(func(current operation_setting.InvoiceSetting) (map[string]string, error) {
		secret := strings.TrimSpace(request.R2SecretAccessKey)
		publicR2Empty := strings.TrimSpace(request.R2Endpoint) == "" && strings.TrimSpace(request.R2Bucket) == "" && strings.TrimSpace(request.R2AccessKeyID) == ""
		if secret == "" && !publicR2Empty {
			secret = current.R2Secret
		}
		setting := operation_setting.InvoiceSetting{
			PersonalEnabled: request.PersonalEnabled, CompanyEnabled: request.CompanyEnabled,
			ApplicationWindowDays: request.ApplicationWindowDays, MinimumAmountMinor: request.MinimumAmountMinor,
			FeePercent: request.FeePercent, PDFRetentionDays: request.PDFRetentionDays,
			R2Endpoint: strings.TrimSpace(request.R2Endpoint), R2Bucket: strings.TrimSpace(request.R2Bucket),
			R2AccessKeyID: strings.TrimSpace(request.R2AccessKeyID), R2Secret: secret,
		}
		if setting.Validate() != nil || setting.R2Configured() && service.ValidateInvoiceR2Configuration(
			setting.R2Endpoint, setting.R2Bucket, setting.R2AccessKeyID, setting.R2Secret,
		) != nil {
			return nil, model.ErrInvoicePaymentSourceInvalidRequest
		}
		return map[string]string{
			"invoice_setting.personal_enabled":        strconv.FormatBool(setting.PersonalEnabled),
			"invoice_setting.company_enabled":         strconv.FormatBool(setting.CompanyEnabled),
			"invoice_setting.application_window_days": strconv.Itoa(setting.ApplicationWindowDays),
			"invoice_setting.minimum_amount_minor":    strconv.FormatInt(setting.MinimumAmountMinor, 10),
			"invoice_setting.fee_percent":             strconv.Itoa(setting.FeePercent),
			"invoice_setting.pdf_retention_days":      strconv.Itoa(setting.PDFRetentionDays),
			"invoice_setting.r2_endpoint":             setting.R2Endpoint,
			"invoice_setting.r2_bucket":               setting.R2Bucket,
			"invoice_setting.r2_access_key_id":        setting.R2AccessKeyID,
			"invoice_setting.r2_secret":               setting.R2Secret,
		}, nil
	})
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, safeInvoiceSetting(operation_setting.GetInvoiceSetting()))
}
