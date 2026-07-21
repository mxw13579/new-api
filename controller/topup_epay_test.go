package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupEpayNotifyControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.SubscriptionOrder{}, &model.Log{}, &model.AffiliateLog{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func withEpayNotifySettings(t *testing.T, key string) {
	t.Helper()

	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay-test-pid"
	operation_setting.EpayKey = key
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	confirmPaymentComplianceForTest(t)
}

func newSignedEpayNotifyRequest(t *testing.T, key string, params map[string]string) *http.Request {
	t.Helper()

	signedParams := epay.GenerateParams(params, key)
	form := url.Values{}
	for k, v := range signedParams {
		form.Set(k, v)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func signedEpayNotifyValues(t *testing.T, key string, params map[string]string) url.Values {
	t.Helper()
	signedParams := epay.GenerateParams(params, key)
	form := url.Values{}
	for k, v := range signedParams {
		form.Set(k, v)
	}
	return form
}

func createEpayNotifyContext(req *http.Request) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = req
	return ctx, recorder
}

func TestEpayNotifyAccountingFailureRespondsFail(t *testing.T) {
	db := setupEpayNotifyControllerTestDB(t)
	const epayKey = "epay-test-key"
	withEpayNotifySettings(t, epayKey)

	tradeNo := "epay-accounting-failure"
	require.NoError(t, db.Create(&model.TopUp{
		UserId:          404,
		Amount:          10,
		Money:           1,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = newSignedEpayNotifyRequest(t, epayKey, map[string]string{
		"pid":          operation_setting.EpayId,
		"type":         "alipay",
		"trade_no":     "epay-platform-trade-no",
		"out_trade_no": tradeNo,
		"name":         "wallet topup",
		"money":        "1.00",
		"trade_status": epay.StatusTradeSuccess,
	})

	EpayNotify(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "fail", recorder.Body.String())

	var topUp model.TopUp
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&topUp).Error)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
}

func TestEpayNotifyRejectsDuplicateFormAndQueryKeys(t *testing.T) {
	testCases := []struct {
		name    string
		request func(t *testing.T, key string, params map[string]string) *http.Request
	}{
		{
			name: "duplicate form key",
			request: func(t *testing.T, key string, params map[string]string) *http.Request {
				form := signedEpayNotifyValues(t, key, params)
				form.Add("trade_no", params["trade_no"])
				req := httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return req
			},
		},
		{
			name: "query and form duplicate",
			request: func(t *testing.T, key string, params map[string]string) *http.Request {
				form := signedEpayNotifyValues(t, key, params)
				req := httptest.NewRequest(http.MethodPost, "/api/user/epay/notify?pid="+url.QueryEscape(params["pid"]), strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return req
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupEpayNotifyControllerTestDB(t)
			const epayKey = "epay-test-key"
			withEpayNotifySettings(t, epayKey)
			params := map[string]string{
				"pid":          operation_setting.EpayId,
				"type":         "alipay",
				"trade_no":     "provider-duplicate",
				"out_trade_no": "merchant-duplicate",
				"name":         "wallet topup",
				"money":        "1.00",
				"trade_status": epay.StatusTradeSuccess,
			}
			ctx, recorder := createEpayNotifyContext(tc.request(t, epayKey, params))

			EpayNotify(ctx)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, "fail", recorder.Body.String())
		})
	}
}

func TestEpayNotifyRequiresVerifiedStatusAndSignedFacts(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(url.Values)
	}{
		{name: "false verify status", mutate: func(form url.Values) { form.Set("sign", "not-a-valid-signature") }},
		{name: "signed money tamper", mutate: func(form url.Values) { form.Set("money", "9.99") }},
		{name: "signed provider id tamper", mutate: func(form url.Values) { form.Set("trade_no", "tampered-provider") }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupEpayNotifyControllerTestDB(t)
			const epayKey = "epay-test-key"
			withEpayNotifySettings(t, epayKey)
			require.NoError(t, db.Create(&model.User{Id: 901, Username: "epay-user", Status: common.UserStatusEnabled, AffCode: "epay-aff"}).Error)
			require.NoError(t, db.Create(&model.TopUp{
				UserId: 901, Amount: 10, Money: 1, TradeNo: "merchant-tamper", PaymentMethod: "alipay",
				PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending,
			}).Error)
			form := signedEpayNotifyValues(t, epayKey, map[string]string{
				"pid": operation_setting.EpayId, "type": "alipay", "trade_no": "provider-tamper",
				"out_trade_no": "merchant-tamper", "name": "wallet topup", "money": "1.00",
				"trade_status": epay.StatusTradeSuccess,
			})
			tc.mutate(form)
			req := httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			ctx, recorder := createEpayNotifyContext(req)

			EpayNotify(ctx)

			assert.Equal(t, "fail", recorder.Body.String())
			var topUp model.TopUp
			require.NoError(t, db.Where("trade_no = ?", "merchant-tamper").First(&topUp).Error)
			assert.Equal(t, common.TopUpStatusPending, topUp.Status)
		})
	}
}

func TestEpayNotifyVerifiedSuccessPersistsSignedProviderIdentity(t *testing.T) {
	db := setupEpayNotifyControllerTestDB(t)
	const epayKey = "epay-test-key"
	withEpayNotifySettings(t, epayKey)
	require.NoError(t, db.Create(&model.User{Id: 902, Username: "epay-success", Status: common.UserStatusEnabled, AffCode: "epay-success-aff"}).Error)
	require.NoError(t, db.Create(&model.TopUp{
		UserId: 902, Amount: 10, Money: 1, TradeNo: "merchant-success", PaymentMethod: "alipay",
		PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending,
	}).Error)
	ctx, recorder := createEpayNotifyContext(newSignedEpayNotifyRequest(t, epayKey, map[string]string{
		"pid": operation_setting.EpayId, "type": "alipay", "trade_no": "  Provider-CaSe  ",
		"out_trade_no": "merchant-success", "name": "wallet topup", "money": "1.00",
		"trade_status": epay.StatusTradeSuccess,
	}))

	EpayNotify(ctx)

	assert.Equal(t, "success", recorder.Body.String())
	var topUp model.TopUp
	require.NoError(t, db.Where("trade_no = ?", "merchant-success").First(&topUp).Error)
	require.NotNil(t, topUp.PaymentProviderTradeNo)
	assert.Equal(t, "Provider-CaSe", *topUp.PaymentProviderTradeNo)
	require.NotNil(t, topUp.PaymentProviderTradeKey)
	require.NotNil(t, topUp.InvoiceEligible)
	assert.True(t, *topUp.InvoiceEligible)
}
