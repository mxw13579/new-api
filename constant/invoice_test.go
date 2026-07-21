package constant

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInvoicePaymentEvidenceConstants(t *testing.T) {
	assert.Equal(t, "legacy_backfill_v1", InvoicePaymentEvidencePolicyVersion)
	assert.Equal(t, 527, len(InvoicePaymentEvidenceCanonicalPolicyJSON))
	assert.Equal(t, "8416fc52942dff7a12f5a285c361e3849a1817865fcf59bc1086187c8d6dde14", InvoicePaymentEvidencePolicySHA256)
	assert.Equal(t, InvoicePaymentEvidencePolicySHA256, fmt.Sprintf("%x", sha256.Sum256([]byte(InvoicePaymentEvidenceCanonicalPolicyJSON))))
	assert.Equal(t, "invoice_payment_evidence_apply", InvoicePaymentEvidenceTaskType)
	assert.Equal(t, "trusted_callback_v1", InvoicePaymentEvidenceSourceTrustedCallback)
	assert.Equal(t, "legacy_backfill_v1", InvoicePaymentEvidenceSourceLegacyBackfill)
	assert.Equal(t, "CNY", InvoicePaymentEvidenceCurrencyCNY)
	assert.Equal(t, "succeeded", InvoicePaymentStateSucceeded)
	assert.Equal(t, "平台额度充值", InvoicePaymentEvidenceTopUpProduct)

	assert.Equal(t, "INVALID_REQUEST", InvoicePaymentEvidenceCodeInvalidRequest)
	assert.Equal(t, "RUN_NOT_FOUND", InvoicePaymentEvidenceCodeRunNotFound)
	assert.Equal(t, "POLICY_MISMATCH", InvoicePaymentEvidenceCodePolicyMismatch)
	assert.Equal(t, "RUN_STATE_CONFLICT", InvoicePaymentEvidenceCodeRunStateConflict)
	assert.Equal(t, "CUTOVER_NOT_READY", InvoicePaymentEvidenceCodeCutoverNotReady)
	assert.Equal(t, "INTERNAL_ERROR", InvoicePaymentEvidenceCodeInternalError)
}
