package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceFeeRefundSettlementSharedContract(t *testing.T) {
	apply := func(int64) (bool, error) { return false, nil }
	handler := newInvoiceFeeRefundSettlementHandler(model.DB, apply, func() int64 { return 123 })
	require.NotNil(t, handler)
	assert.Equal(t, model.InvoiceFeeRefundSettlementTaskType, handler.Type())
	assert.Same(t, model.DB, handler.db)
	assert.NotNil(t, handler.apply)
	assert.Equal(t, int64(123), handler.now())

	production := NewInvoiceFeeRefundSettlementHandler()
	require.NotNil(t, production)
	assert.Equal(t, model.InvoiceFeeRefundSettlementTaskType, production.Type())

	result := InvoiceFeeRefundSettlementResult{
		Scanned: 4, Applied: 1, StillPending: 2, Failed: 1,
	}
	assert.Equal(t, 4, result.Scanned)
	assert.Equal(t, 1, result.Applied)
	assert.Equal(t, 2, result.StillPending)
	assert.Equal(t, 1, result.Failed)
}
