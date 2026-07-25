package dto_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceFeeHistoryJSONContainsOnlyPublicLedgerFields(t *testing.T) {
	payload, err := common.Marshal(dto.InvoiceFeeHistoryItem{ID: 1, ApplicationID: 2, ApplicationNo: "INV-2", FeePercent: 5})
	require.NoError(t, err)
	encoded := string(payload)
	for _, forbidden := range []string{"profile_snapshot", "policy_snapshot", "tax_number", "object_key", "r2_", "secret"} {
		assert.NotContains(t, encoded, forbidden)
	}
	assert.Contains(t, encoded, `"fee_percent":5`)
}
