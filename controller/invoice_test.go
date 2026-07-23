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
	Success bool `json:"success"`
	Data    struct {
		Code string `json:"code"`
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

func TestInvoiceDownloadControllerRedirectsWithoutLeakingErrors(t *testing.T) {
	previous := getInvoiceDocumentDownload
	t.Cleanup(func() { getInvoiceDocumentDownload = previous })

	getInvoiceDocumentDownload = func(context.Context, int, int64) (string, error) {
		return "https://private.example.test/invoice.pdf?signature=test-only", nil
	}
	requestContext, recorder := invoiceControllerContext(http.MethodGet, "/", "")
	requestContext.Params = gin.Params{{Key: "id", Value: "7"}}
	requestContext.Set("id", 11)
	DownloadInvoiceDocument(requestContext)
	assert.Equal(t, http.StatusFound, recorder.Code)
	assert.Equal(t, "https://private.example.test/invoice.pdf?signature=test-only", recorder.Header().Get("Location"))

	getInvoiceDocumentDownload = func(context.Context, int, int64) (string, error) {
		return "", fmt.Errorf("%w: https://private.example.test/invoice.pdf?signature=secret", service.ErrInvoiceObjectRetryable)
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

func TestUpdateInvoiceSettingRejectsUnknownFieldsAndPersistsValidSetting(t *testing.T) {
	setupInvoiceControllerDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	previous := *operation_setting.GetInvoiceSetting()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		*operation_setting.GetInvoiceSetting() = previous
		common.OptionMap = previousOptionMap
	})

	context, recorder := invoiceControllerContext(http.MethodPut, "/", `{"personal_enabled":true,"company_enabled":true,"application_window_days":30,"minimum_amount_minor":0,"fee_quota":0,"pdf_retention_days":30,"unexpected":true}`)
	UpdateInvoiceSetting(context)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	context, recorder = invoiceControllerContext(http.MethodPut, "/", `{"personal_enabled":true,"company_enabled":true,"application_window_days":45,"minimum_amount_minor":100,"fee_quota":20,"pdf_retention_days":60}`)
	UpdateInvoiceSetting(context)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 45, operation_setting.GetInvoiceSetting().ApplicationWindowDays)
	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", "invoice_setting.application_window_days").Error)
	assert.Equal(t, "45", option.Value)

	context, recorder = invoiceControllerContext(http.MethodGet, "/", "")
	GetInvoiceSetting(context)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"application_window_days":45`)
}
