package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceFeeRefundSettlementSharedContract(t *testing.T) {
	production := NewInvoiceFeeRefundSettlementHandler()
	require.NotNil(t, production)
	assert.Equal(t, model.InvoiceFeeRefundSettlementTaskType, production.Type())
	scheduled, ok := production.(ScheduledSystemTaskHandler)
	require.True(t, ok)
	assert.True(t, scheduled.Enabled())
	assert.Equal(t, time.Minute, scheduled.Interval())
	assert.Nil(t, scheduled.NewPayload())

	result := InvoiceFeeRefundSettlementResult{
		Scanned: 4, Applied: 1, StillPending: 2, Failed: 1,
	}
	encoded, err := common.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, `{"scanned":4,"applied":1,"still_pending":2,"failed":1}`, string(encoded))
}
