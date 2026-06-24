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

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.AffiliateLog{}))

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
