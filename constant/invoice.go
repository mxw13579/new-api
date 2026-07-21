package constant

const (
	InvoicePaymentEvidencePolicyVersion         = "legacy_backfill_v1"
	InvoicePaymentEvidenceCanonicalPolicyJSON   = `{"candidate":{"amount_gt":0,"complete_time_gt":0,"money_finite":true,"money_gt":0,"payment_method_trim_nonempty":true,"status":"success","subscription_trade_no_not_exists":true,"trade_no_trim_nonempty":true},"include_epay_admin_completed_success":true,"money_field":"TopUp.Money","payment_provider":"epay","policy_version":"legacy_backfill_v1","rate_denominator":1,"rate_numerator":1,"source_currency":"CNY","target_currency":"CNY","wire_format":{"bit_size":64,"format":"f","precision":2},"wire_function":"strconv.FormatFloat"}`
	InvoicePaymentEvidencePolicySHA256          = "8416fc52942dff7a12f5a285c361e3849a1817865fcf59bc1086187c8d6dde14"
	InvoicePaymentEvidenceTaskType              = "invoice_payment_evidence_apply"
	InvoicePaymentEvidenceSourceTrustedCallback = "trusted_callback_v1"
	InvoicePaymentEvidenceSourceLegacyBackfill  = "legacy_backfill_v1"
	InvoicePaymentEvidenceCurrencyCNY           = "CNY"
	InvoicePaymentStateSucceeded                = "succeeded"
	InvoicePaymentEvidenceTopUpProduct          = "平台额度充值"

	InvoicePaymentEvidenceCodeInvalidRequest   = "INVALID_REQUEST"
	InvoicePaymentEvidenceCodeRunNotFound      = "RUN_NOT_FOUND"
	InvoicePaymentEvidenceCodePolicyMismatch   = "POLICY_MISMATCH"
	InvoicePaymentEvidenceCodeRunStateConflict = "RUN_STATE_CONFLICT"
	InvoicePaymentEvidenceCodeCutoverNotReady  = "CUTOVER_NOT_READY"
	InvoicePaymentEvidenceCodeInternalError    = "INTERNAL_ERROR"
)
