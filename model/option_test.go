package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRechargeRebateRatioForInviterOptionMapDefault(t *testing.T) {
	originalOptionMap := common.OptionMap
	originalRatio := common.RechargeRebateRatioForInviter
	common.RechargeRebateRatioForInviter = 0
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
		common.RechargeRebateRatioForInviter = originalRatio
	})

	InitOptionMap()

	require.Equal(t, "0", common.OptionMap["RechargeRebateRatioForInviter"])
}

func TestUpdateOptionMapParsesRechargeRebateRatioForInviter(t *testing.T) {
	originalOptionMap := common.OptionMap
	originalRatio := common.RechargeRebateRatioForInviter
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
		common.RechargeRebateRatioForInviter = originalRatio
	})

	err := updateOptionMap("RechargeRebateRatioForInviter", "5")

	require.NoError(t, err)
	require.Equal(t, "5", common.OptionMap["RechargeRebateRatioForInviter"])
	require.Equal(t, 5.0, common.RechargeRebateRatioForInviter)
}
