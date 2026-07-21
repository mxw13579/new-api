package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInvoicePaymentEvidenceControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&model.TopUp{}, &model.SubscriptionOrder{}, &model.SystemTask{}, &model.SystemTaskLock{},
		&model.InvoicePaymentEvidenceBackfillRun{}, &model.InvoicePaymentEvidenceBackfillItem{},
	))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func invoiceEvidenceRequestContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 1)
	return ctx, recorder
}

func TestInvoiceEvidencePOSTControllersUseStrictObjectContracts(t *testing.T) {
	setupInvoicePaymentEvidenceControllerTest(t)
	testCases := []struct {
		name   string
		body   string
		invoke func(*gin.Context)
	}{
		{name: "preview duplicate", body: `{"deployment_sha":"0123456789abcdef0123456789abcdef01234567","deployment_sha":"0123456789abcdef0123456789abcdef01234567","active_instance_count":1,"matching_instance_count":1,"disallowed_instance_count":0}`, invoke: PreviewInvoicePaymentEvidenceBackfill},
		{name: "preview unknown", body: `{"deployment_sha":"0123456789abcdef0123456789abcdef01234567","active_instance_count":1,"matching_instance_count":1,"disallowed_instance_count":0,"extra":true}`, invoke: PreviewInvoicePaymentEvidenceBackfill},
		{name: "apply blank", body: `{"expected_policy_sha256":""}`, invoke: ApplyInvoicePaymentEvidenceBackfill},
		{name: "apply trailing", body: `{"expected_policy_sha256":"abc"}{}`, invoke: ApplyInvoicePaymentEvidenceBackfill},
		{name: "stop whitespace only", body: "   ", invoke: StopInvoicePaymentEvidenceBackfill},
		{name: "stop nonempty object", body: `{"reason":"operator"}`, invoke: StopInvoicePaymentEvidenceBackfill},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, recorder := invoiceEvidenceRequestContext(http.MethodPost, "/", tc.body)
			ctx.Params = gin.Params{{Key: "run_id", Value: "1"}}
			tc.invoke(ctx)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, `{"success":false,"code":"INVALID_REQUEST","message":"invalid request"}`, recorder.Body.String())
		})
	}
}

func TestInvoiceEvidenceControllerSuccessEnvelopes(t *testing.T) {
	db := setupInvoicePaymentEvidenceControllerTest(t)
	require.NoError(t, db.Create(&model.TopUp{
		UserId: 1, Amount: 10, Money: 1, TradeNo: "controller-preview", PaymentMethod: "alipay",
		PaymentProvider: model.PaymentProviderEpay, CompleteTime: 10, Status: common.TopUpStatusSuccess,
	}).Error)
	previewCtx, previewRecorder := invoiceEvidenceRequestContext(http.MethodPost, "/", `{"deployment_sha":"0123456789abcdef0123456789abcdef01234567","active_instance_count":1,"matching_instance_count":1,"disallowed_instance_count":0}`)
	PreviewInvoicePaymentEvidenceBackfill(previewCtx)
	require.Equal(t, http.StatusCreated, previewRecorder.Code)
	var previewEnvelope struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			RunID        int64  `json:"run_id"`
			PolicySHA256 string `json:"policy_sha256"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(previewRecorder.Body.Bytes(), &previewEnvelope))
	assert.True(t, previewEnvelope.Success)
	assert.Empty(t, previewEnvelope.Message)
	assert.Equal(t, constant.InvoicePaymentEvidencePolicySHA256, previewEnvelope.Data.PolicySHA256)

	runID := strconv.FormatInt(previewEnvelope.Data.RunID, 10)
	applyCtx, applyRecorder := invoiceEvidenceRequestContext(http.MethodPost, "/", `{"expected_policy_sha256":"`+constant.InvoicePaymentEvidencePolicySHA256+`"}`)
	applyCtx.Params = gin.Params{{Key: "run_id", Value: runID}}
	ApplyInvoicePaymentEvidenceBackfill(applyCtx)
	assert.Equal(t, http.StatusAccepted, applyRecorder.Code)

	statusCtx, statusRecorder := invoiceEvidenceRequestContext(http.MethodGet, "/", "")
	statusCtx.Params = gin.Params{{Key: "run_id", Value: runID}}
	GetInvoicePaymentEvidenceBackfill(statusCtx)
	assert.Equal(t, http.StatusOK, statusRecorder.Code)

	stopCtx, stopRecorder := invoiceEvidenceRequestContext(http.MethodPost, "/", "")
	stopCtx.Params = gin.Params{{Key: "run_id", Value: runID}}
	StopInvoicePaymentEvidenceBackfill(stopCtx)
	assert.Equal(t, http.StatusOK, stopRecorder.Code)
	assert.Contains(t, stopRecorder.Body.String(), `"reason":"operator_stopped"`)
}
