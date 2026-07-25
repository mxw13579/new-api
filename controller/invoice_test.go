package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceControllerEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Code        string `json:"code"`
		DownloadURL string `json:"download_url"`
	} `json:"data"`
}

func setupInvoiceControllerDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
}

func invoiceControllerContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context, recorder
}

func TestInvoiceControllersRejectUnknownJSONFields(t *testing.T) {
	context, recorder := invoiceControllerContext(http.MethodPost, "/", `{"type":"personal","title":"Alice","tax_number":"","is_default":true,"unexpected":true}`)
	context.Set("id", 7)

	CreateInvoiceProfile(context)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	var envelope invoiceControllerEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	assert.False(t, envelope.Success)
	assert.Equal(t, constant.InvoiceCodeInvalidRequest, envelope.Data.Code)
}

func TestInvoiceControllerCrossUserLookupIsNotFound(t *testing.T) {
	setupInvoiceControllerDB(t)
	application := model.InvoiceApplication{
		ApplicationNo: "INV-PRIVATE", UserID: 11, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypeCompany, Status: constant.InvoiceApplicationStatusSubmitted,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{"title":"Secret Title","tax_number":"Secret Tax"}`,
		PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, model.DB.Create(&application).Error)
	context, recorder := invoiceControllerContext(http.MethodGet, "/", "")
	context.Params = gin.Params{{Key: "id", Value: "1"}}
	context.Set("id", 12)

	GetInvoiceApplication(context)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "Secret Title")
	assert.NotContains(t, recorder.Body.String(), "Secret Tax")
	var envelope invoiceControllerEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	assert.Equal(t, constant.InvoiceCodeNotFound, envelope.Data.Code)
}

func TestInvoiceControllerRejectsInvalidPagination(t *testing.T) {
	context, recorder := invoiceControllerContext(http.MethodGet, "/?page=0&page_size=20", "")
	context.Set("id", 7)

	ListInvoiceApplications(context)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	var envelope invoiceControllerEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	assert.Equal(t, constant.InvoiceCodeInvalidRequest, envelope.Data.Code)
}

func TestInvoiceDownloadControllerMasksCrossUserAndDoesNotLeakURLs(t *testing.T) {
	setupInvoiceControllerDB(t)
	application := model.InvoiceApplication{
		ApplicationNo: "INV-PRIVATE-DOWNLOAD", UserID: 11, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusIssued,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, model.DB.Create(&application).Error)
	for _, applicationID := range []int64{application.ID, application.ID + 999} {
		context, recorder := invoiceControllerContext(http.MethodGet, "/", "")
		context.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(applicationID, 10)}}
		context.Set("id", 12)

		DownloadInvoiceDocument(context)

		assert.Equal(t, http.StatusNotFound, recorder.Code)
		assert.NotContains(t, recorder.Body.String(), "http")
		assert.NotContains(t, recorder.Body.String(), "token")
		assert.Contains(t, recorder.Body.String(), constant.InvoiceCodeNotFound)
	}
}

func TestInvoiceDownloadControllerReturnsVerifiedBinaryWithPrivateHeaders(t *testing.T) {
	previous := getInvoiceDocumentDownload
	t.Cleanup(func() { getInvoiceDocumentDownload = previous })

	getInvoiceDocumentDownload = func(context.Context, int, int64) ([]byte, error) {
		return []byte("%PDF-verified"), nil
	}
	requestContext, recorder := invoiceControllerContext(http.MethodGet, "/", "")
	requestContext.Params = gin.Params{{Key: "id", Value: "7"}}
	requestContext.Set("id", 11)
	DownloadInvoiceDocument(requestContext)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/pdf", recorder.Header().Get("Content-Type"))
	assert.Equal(t, `attachment; filename="invoice.pdf"`, recorder.Header().Get("Content-Disposition"))
	assert.Equal(t, strconv.Itoa(len("%PDF-verified")), recorder.Header().Get("Content-Length"))
	assert.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
	assert.Empty(t, recorder.Header().Get("Location"))
	assert.Equal(t, "%PDF-verified", recorder.Body.String())

	getInvoiceDocumentDownload = func(context.Context, int, int64) ([]byte, error) {
		return nil, fmt.Errorf("%w: https://private.example.test/invoice.pdf?signature=secret", service.ErrInvoiceObjectRetryable)
	}
	requestContext, recorder = invoiceControllerContext(http.MethodGet, "/", "")
	requestContext.Params = gin.Params{{Key: "id", Value: "7"}}
	requestContext.Set("id", 11)
	DownloadInvoiceDocument(requestContext)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Location"))
	assert.NotContains(t, recorder.Body.String(), "private.example.test")
	assert.NotContains(t, recorder.Body.String(), "signature")
	assert.Contains(t, recorder.Body.String(), constant.InvoiceCodeInternalError)
}

func TestInvoiceErrorMappingDistinguishesUnavailableFromInfrastructure(t *testing.T) {
	code, status := invoiceErrorCode(service.ErrInvoiceDocumentUnavailable)
	assert.Equal(t, constant.InvoiceCodeDocumentUnavailable, code)
	assert.Equal(t, http.StatusConflict, status)

	for _, err := range []error{service.ErrInvoiceCommitAmbiguous, service.ErrInvoiceDocumentRetryable, service.ErrInvoiceObjectNotFound, service.ErrInvoiceObjectRetryable, service.ErrInvoiceObjectTerminal} {
		code, status = invoiceErrorCode(err)
		assert.Equal(t, constant.InvoiceCodeInternalError, code)
		assert.Equal(t, http.StatusInternalServerError, status)
	}
	context, recorder := invoiceControllerContext(http.MethodGet, "/", "")
	writeInvoiceError(context, fmt.Errorf("%w: https://signed.example.test/private-token", service.ErrInvoiceObjectRetryable))
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "signed.example.test")
	assert.NotContains(t, recorder.Body.String(), "private-token")
}

func TestInvoiceErrorMappingDistinguishesIssuanceAndStateConflicts(t *testing.T) {
	code, status := invoiceErrorCode(model.ErrInvoiceIssuanceConflict)
	assert.Equal(t, constant.InvoiceCodeIssuanceConflict, code)
	assert.Equal(t, http.StatusConflict, status)
	previousTranslate := common.TranslateMessage
	common.TranslateMessage = func(_ *gin.Context, key string, _ ...map[string]any) string { return key }
	t.Cleanup(func() { common.TranslateMessage = previousTranslate })
	context, recorder := invoiceControllerContext(http.MethodPost, "/", "")
	writeInvoiceError(context, model.ErrInvoiceIssuanceConflict)
	var envelope invoiceControllerEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	assert.Equal(t, i18n.MsgInvoiceIssuanceConflict, envelope.Message)

	code, status = invoiceErrorCode(model.ErrInvoiceStateConflict)
	assert.Equal(t, constant.InvoiceCodeStateConflict, code)
	assert.Equal(t, http.StatusConflict, status)
}

func TestUpdateInvoiceSettingRejectsUnknownFieldsAndPersistsValidSetting(t *testing.T) {
	setupInvoiceControllerDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	previous := operation_setting.GetInvoiceSetting()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		operation_setting.PublishInvoiceSetting(previous)
		common.OptionMap = previousOptionMap
	})

	context, recorder := invoiceControllerContext(http.MethodPut, "/", `{"personal_enabled":true,"company_enabled":true,"application_window_days":30,"minimum_amount_minor":0,"fee_percent":0,"pdf_retention_days":30,"r2_endpoint":"","r2_bucket":"","r2_access_key_id":"","r2_secret_access_key":"","unexpected":true}`)
	UpdateInvoiceSetting(context)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	context, recorder = invoiceControllerContext(http.MethodPut, "/", `{"personal_enabled":true,"company_enabled":true,"application_window_days":30,"minimum_amount_minor":0,"fee_percent":0,"pdf_retention_days":30,"r2_endpoint":"","r2_bucket":"","r2_access_key_id":"","r2_secret_configured":true,"r2_secret_access_key":""}`)
	UpdateInvoiceSetting(context)
	assert.Equal(t, http.StatusBadRequest, recorder.Code, "response-only secret state must not be accepted by PUT")

	context, recorder = invoiceControllerContext(http.MethodPut, "/", `{"personal_enabled":true,"company_enabled":true,"application_window_days":45,"minimum_amount_minor":100,"fee_percent":5,"pdf_retention_days":60,"r2_endpoint":"https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com","r2_bucket":"private-invoices","r2_access_key_id":"access-id","r2_secret_access_key":"secret-value"}`)
	UpdateInvoiceSetting(context)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 45, operation_setting.GetInvoiceSetting().ApplicationWindowDays)
	assert.Equal(t, 5, operation_setting.GetInvoiceSetting().FeePercent)
	assert.Equal(t, "secret-value", operation_setting.GetInvoiceSetting().R2Secret)
	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", "invoice_setting.application_window_days").Error)
	assert.Equal(t, "45", option.Value)
	var secretOption model.Option
	require.NoError(t, model.DB.First(&secretOption, "key = ?", "invoice_setting.r2_secret").Error)
	assert.Equal(t, "secret-value", secretOption.Value)
	var bucketOption model.Option
	require.NoError(t, model.DB.First(&bucketOption, "key = ?", "invoice_setting.r2_bucket").Error)
	store, err := service.NewInvoiceR2StoreFromSetting()
	require.NoError(t, err)
	assert.Equal(t, bucketOption.Value, operation_setting.GetInvoiceSetting().R2Bucket)
	assert.Equal(t, bucketOption.Value, store.Bucket())

	context, recorder = invoiceControllerContext(http.MethodGet, "/", "")
	GetInvoiceSetting(context)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"application_window_days":45`)
	assert.Contains(t, recorder.Body.String(), `"fee_percent":5`)
	assert.Contains(t, recorder.Body.String(), `"r2_secret_configured":true`)
	assert.NotContains(t, recorder.Body.String(), "secret-value")

	context, recorder = invoiceControllerContext(http.MethodPut, "/", `{"personal_enabled":true,"company_enabled":true,"application_window_days":45,"minimum_amount_minor":100,"fee_percent":6,"pdf_retention_days":60,"r2_endpoint":"https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com","r2_bucket":"private-invoices","r2_access_key_id":"rotated-access","r2_secret_access_key":""}`)
	UpdateInvoiceSetting(context)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "secret-value", operation_setting.GetInvoiceSetting().R2Secret)

	context, recorder = invoiceControllerContext(http.MethodPut, "/", `{"personal_enabled":true,"company_enabled":true,"application_window_days":45,"minimum_amount_minor":100,"fee_percent":6,"pdf_retention_days":60,"r2_endpoint":"","r2_bucket":"","r2_access_key_id":"","r2_secret_access_key":""}`)
	UpdateInvoiceSetting(context)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, operation_setting.GetInvoiceSetting().R2Secret)
	assert.False(t, operation_setting.GetInvoiceSetting().FeePercentMigrationRequired)
}

func TestInvoiceR2ConfigurationErrorHasExplicitCode(t *testing.T) {
	code, status := invoiceErrorCode(service.ErrInvoiceR2NotConfigured)
	assert.Equal(t, constant.InvoiceCodeStorageNotConfigured, code)
	assert.Equal(t, http.StatusServiceUnavailable, status)
}
