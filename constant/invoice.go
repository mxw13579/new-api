package constant

const (
	InvoiceCurrencyCNY = "CNY"

	InvoiceTypePersonal = "personal"
	InvoiceTypeCompany  = "company"

	InvoiceApplicationStatusSubmitted = "submitted"
	InvoiceApplicationStatusReviewing = "reviewing"
	InvoiceApplicationStatusApproved  = "approved"
	InvoiceApplicationStatusRejected  = "rejected"
	InvoiceApplicationStatusCancelled = "cancelled"
	InvoiceApplicationStatusIssued    = "issued"

	InvoicePaymentReviewStatusNone           = "none"
	InvoicePaymentReviewStatusPreIssueHold   = "pre_issue_hold"
	InvoicePaymentReviewStatusPostIssueHold  = "post_issue_hold"
	InvoicePaymentReviewStatusResolvedValid  = "resolved_valid"
	InvoicePaymentReviewStatusResolvedVoided = "resolved_voided"

	InvoiceFeeStatusNotRequired   = "not_required"
	InvoiceFeeStatusPaid          = "paid"
	InvoiceFeeStatusRefundPending = "refund_pending"
	InvoiceFeeStatusRefunded      = "refunded"

	InvoiceDocumentStatusUploading    = "uploading"
	InvoiceDocumentStatusValidating   = "validating"
	InvoiceDocumentStatusAvailable    = "available"
	InvoiceDocumentStatusSuperseded   = "superseded"
	InvoiceDocumentStatusUploadFailed = "upload_failed"
	InvoiceDocumentStatusDeleting     = "deleting"
	InvoiceDocumentStatusDeleted      = "deleted"
	InvoiceDocumentStatusDeleteFailed = "delete_failed"
	InvoiceDocumentStatusMissing      = "missing"

	InvoiceCodeInvalidRequest          = "INVOICE_INVALID_REQUEST"
	InvoiceCodeForbidden               = "INVOICE_FORBIDDEN"
	InvoiceCodeQuotaInsufficient       = "INVOICE_QUOTA_INSUFFICIENT"
	InvoiceCodeNotFound                = "INVOICE_NOT_FOUND"
	InvoiceCodeIdempotencyConflict     = "INVOICE_IDEMPOTENCY_CONFLICT"
	InvoiceCodeStateConflict           = "INVOICE_STATE_CONFLICT"
	InvoiceCodeTopUpIneligible         = "INVOICE_TOPUP_INELIGIBLE"
	InvoiceCodePaymentEvidenceConflict = "INVOICE_PAYMENT_EVIDENCE_CONFLICT"
	InvoiceCodeDocumentUnavailable     = "INVOICE_DOCUMENT_UNAVAILABLE"
	InvoiceCodeInternalError           = "INVOICE_INTERNAL_ERROR"

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
