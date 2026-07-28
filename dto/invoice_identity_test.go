package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceIdentityFieldsAreOmittedFromOwnerProjections(t *testing.T) {
	ownerJSON, err := common.Marshal(InvoiceApplicationSummary{ID: 1})
	require.NoError(t, err)
	assert.NotContains(t, string(ownerJSON), "user_id")
	assert.NotContains(t, string(ownerJSON), "username")
	assert.NotContains(t, string(ownerJSON), "display_name")

	userID := 42
	adminJSON, err := common.Marshal(InvoiceApplicationSummary{ID: 1, UserID: &userID})
	require.NoError(t, err)
	assert.Contains(t, string(adminJSON), `"user_id":42`)
}

func TestInvoiceFeeIdentityFieldsAreOmittedFromOwnerLedger(t *testing.T) {
	ownerJSON, err := common.Marshal(InvoiceFeeHistoryItem{ID: 1})
	require.NoError(t, err)
	assert.NotContains(t, string(ownerJSON), "user_id")
	assert.NotContains(t, string(ownerJSON), "username")
	assert.NotContains(t, string(ownerJSON), "display_name")
}
