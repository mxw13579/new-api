package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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
