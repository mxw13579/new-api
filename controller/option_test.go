package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestValidateRechargeRebateRatioForInviterAllowsBoundaryValues(t *testing.T) {
	tests := []string{"0", "5", "100"}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			require.NoError(t, validateRechargeRebateRatioForInviter(value))
		})
	}
}

func TestValidateRechargeRebateRatioForInviterRejectsInvalidValues(t *testing.T) {
	tests := []string{"-0.01", "100.01", "not-a-number"}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			require.Error(t, validateRechargeRebateRatioForInviter(value))
		})
	}
}

func TestUpdateOptionRequiresPaymentComplianceForPositiveRechargeRebateRatio(t *testing.T) {
	setupOptionControllerTestDB(t)
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = ""

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/option/", OptionUpdateRequest{
		Key:   "RechargeRebateRatioForInviter",
		Value: "5",
	}, 1)

	UpdateOption(ctx)

	response := decodeAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Equal(t, "payment.compliance_required", response.Message)
}

func TestUpdateOptionAllowsZeroRechargeRebateRatioWithoutPaymentCompliance(t *testing.T) {
	setupOptionControllerTestDB(t)
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = ""

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/option/", OptionUpdateRequest{
		Key:   "RechargeRebateRatioForInviter",
		Value: "0",
	}, 1)

	UpdateOption(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success)
}

func setupOptionControllerTestDB(t *testing.T) {
	t.Helper()
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))

	oldOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		common.OptionMap = oldOptionMap
	})
}
