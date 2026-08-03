package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestFormatUserLogsStripsRawStreamErrorsFromUserResponseOnly(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"stream_status": map[string]interface{}{
			"status":      "error",
			"end_reason":  "upstream_error",
			"error_count": 2,
			"end_error":   "dial tcp 10.0.0.8:443: connection refused",
			"errors":      []string{"provider request id secret", "raw SDK diagnostic"},
		},
	})
	userLogs := []*Log{{Other: other}}
	adminLog := &Log{Other: other}

	formatUserLogs(userLogs, 0)

	parsed, err := common.StrToMap(userLogs[0].Other)
	require.NoError(t, err)
	streamStatus, ok := parsed["stream_status"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "error", streamStatus["status"])
	assert.Equal(t, "upstream_error", streamStatus["end_reason"])
	assert.EqualValues(t, 2, streamStatus["error_count"])
	assert.NotContains(t, streamStatus, "end_error")
	assert.NotContains(t, streamStatus, "errors")
	assert.EqualValues(t, 0.004, parsed["model_price"])

	adminOther, err := common.StrToMap(adminLog.Other)
	require.NoError(t, err)
	adminStreamStatus, ok := adminOther["stream_status"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "dial tcp 10.0.0.8:443: connection refused", adminStreamStatus["end_error"])
	assert.ElementsMatch(t, []interface{}{"provider request id secret", "raw SDK diagnostic"}, adminStreamStatus["errors"])
}
