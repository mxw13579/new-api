package controller

import (
	"net/http"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

func uniqueEpayFormParams(request *http.Request) (map[string]string, bool) {
	if request == nil || request.ParseForm() != nil || len(request.Form) == 0 {
		return nil, false
	}
	params := make(map[string]string, len(request.Form))
	for key, values := range request.Form {
		if len(values) != 1 {
			return nil, false
		}
		params[key] = values[0]
	}
	return params, true
}

func signedEpayAmountMinor(value string) (int64, bool) {
	amount, err := decimal.NewFromString(value)
	if err != nil {
		return 0, false
	}
	amount = amount.Shift(2)
	if amount.LessThanOrEqual(decimal.Zero) || !amount.Equal(amount.Truncate(0)) {
		return 0, false
	}
	minor := amount.IntPart()
	return minor, decimal.NewFromInt(minor).Equal(amount)
}

func verifiedEpayCompletion(request *http.Request) (model.VerifiedEpayCompletion, bool) {
	params, ok := uniqueEpayFormParams(request)
	client := GetEpayClient()
	if !ok || client == nil {
		return model.VerifiedEpayCompletion{}, false
	}
	result, err := client.Verify(params)
	if err != nil || result == nil || !result.VerifyStatus || params["pid"] != operation_setting.EpayId ||
		result.TradeStatus != epay.StatusTradeSuccess || !operation_setting.ContainsPayMethod(result.Type) {
		return model.VerifiedEpayCompletion{}, false
	}
	providerTradeNo, providerTradeKey, err := model.NormalizeEpayProviderTradeIdentity(result.TradeNo)
	if err != nil {
		return model.VerifiedEpayCompletion{}, false
	}
	paidAmountMinor, ok := signedEpayAmountMinor(result.Money)
	if !ok {
		return model.VerifiedEpayCompletion{}, false
	}
	return model.VerifiedEpayCompletion{
		MerchantTradeNo: result.ServiceTradeNo, ProviderTradeNo: providerTradeNo,
		ProviderTradeKey: providerTradeKey, Method: result.Type,
		PaidAmountMinor: paidAmountMinor, AcceptedAt: common.GetTimestamp(),
	}, true
}
