package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoicePaymentEvidenceRootOnlyPermissions(t *testing.T) {
	assert.Equal(t, "invoice_payment_evidence", ResourceInvoicePaymentEvidence)
	assert.Equal(t, Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionRead}, InvoicePaymentEvidenceRead)
	assert.Equal(t, Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionOperate}, InvoicePaymentEvidenceOperate)

	var definition *ResourceDefinition
	for _, candidate := range Catalog() {
		if candidate.Resource == ResourceInvoicePaymentEvidence {
			candidate := candidate
			definition = &candidate
			break
		}
	}
	require.NotNil(t, definition)
	require.Len(t, definition.Actions, 2)
	assert.Equal(t, ActionRead, definition.Actions[0].Action)
	assert.Empty(t, definition.Actions[0].DefaultRoles)
	assert.Equal(t, ActionOperate, definition.Actions[1].Action)
	assert.Empty(t, definition.Actions[1].DefaultRoles)

	assert.NotContains(t, PermissionsForRole(BuiltInRoleAdmin), InvoicePaymentEvidenceRead)
	assert.NotContains(t, PermissionsForRole(BuiltInRoleAdmin), InvoicePaymentEvidenceOperate)

	rootGrants := roleGrants(RoleSpec{Key: BuiltInRoleRoot, Superuser: true})
	assert.True(t, rootGrants[ResourceInvoicePaymentEvidence][ActionRead])
	assert.True(t, rootGrants[ResourceInvoicePaymentEvidence][ActionOperate])
}
