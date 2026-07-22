package constant

const (
	// InvoiceCurrencyCNY is the only currency accepted by the personal-invoice contract.
	InvoiceCurrencyCNY = "CNY"

	// InvoiceTypePersonal identifies an invoice issued to an individual profile.
	InvoiceTypePersonal = "personal"
	// InvoiceTypeCompany identifies an invoice issued to a company profile.
	InvoiceTypeCompany = "company"

	// InvoiceApplicationStatusSubmitted marks an application awaiting review.
	InvoiceApplicationStatusSubmitted = "submitted"
	// InvoiceApplicationStatusReviewing marks an application under active review.
	InvoiceApplicationStatusReviewing = "reviewing"
	// InvoiceApplicationStatusApproved marks an application eligible for initial document issuance.
	InvoiceApplicationStatusApproved = "approved"
	// InvoiceApplicationStatusRejected marks an application declined before issuance.
	InvoiceApplicationStatusRejected = "rejected"
	// InvoiceApplicationStatusCancelled marks a user-cancelled application.
	InvoiceApplicationStatusCancelled = "cancelled"
	// InvoiceApplicationStatusIssued marks an application with an activated invoice document.
	InvoiceApplicationStatusIssued = "issued"

	// InvoicePaymentReviewStatusNone indicates no payment-evidence restriction.
	InvoicePaymentReviewStatusNone = "none"
	// InvoicePaymentReviewStatusPreIssueHold blocks issuance while payment evidence is reviewed.
	InvoicePaymentReviewStatusPreIssueHold = "pre_issue_hold"
	// InvoicePaymentReviewStatusPostIssueHold blocks download after issuance pending payment review.
	InvoicePaymentReviewStatusPostIssueHold = "post_issue_hold"
	// InvoicePaymentReviewStatusResolvedValid restores issuance and download eligibility.
	InvoicePaymentReviewStatusResolvedValid = "resolved_valid"
	// InvoicePaymentReviewStatusResolvedVoided permanently suppresses the invoice document.
	InvoicePaymentReviewStatusResolvedVoided = "resolved_voided"

	// InvoiceFeeStatusNotRequired indicates that the snapshotted invoice fee was zero.
	InvoiceFeeStatusNotRequired = "not_required"
	// InvoiceFeeStatusPaid indicates that the invoice fee was charged successfully.
	InvoiceFeeStatusPaid = "paid"
	// InvoiceFeeStatusRefundPending indicates that a fee refund obligation awaits settlement.
	InvoiceFeeStatusRefundPending = "refund_pending"
	// InvoiceFeeStatusRefunded indicates that the invoice fee was returned.
	InvoiceFeeStatusRefunded = "refunded"

	// InvoiceDocumentStatusUploading marks a PDF being streamed to private staging storage.
	InvoiceDocumentStatusUploading = "uploading"
	// InvoiceDocumentStatusValidating marks a promoted PDF awaiting atomic activation.
	InvoiceDocumentStatusValidating = "validating"
	// InvoiceDocumentStatusAvailable marks the active PDF as eligible for bounded download.
	InvoiceDocumentStatusAvailable = "available"
	// InvoiceDocumentStatusSuperseded marks a replaced PDF queued for deletion.
	InvoiceDocumentStatusSuperseded = "superseded"
	// InvoiceDocumentStatusUploadFailed marks a PDF that failed validation or promotion.
	InvoiceDocumentStatusUploadFailed = "upload_failed"
	// InvoiceDocumentStatusDeleting marks a PDF held by an active cleanup lease.
	InvoiceDocumentStatusDeleting = "deleting"
	// InvoiceDocumentStatusDeleted marks a PDF whose private object was removed.
	InvoiceDocumentStatusDeleted = "deleted"
	// InvoiceDocumentStatusDeleteFailed marks a PDF awaiting another cleanup attempt.
	InvoiceDocumentStatusDeleteFailed = "delete_failed"
	// InvoiceDocumentStatusMissing represents an application without an active document.
	InvoiceDocumentStatusMissing = "missing"

	// InvoiceCodeInvalidRequest identifies malformed or semantically invalid invoice input.
	InvoiceCodeInvalidRequest = "INVOICE_INVALID_REQUEST"
	// InvoiceCodeForbidden identifies a missing invoice-specific permission.
	InvoiceCodeForbidden = "INVOICE_FORBIDDEN"
	// InvoiceCodeQuotaInsufficient identifies insufficient wallet quota for the invoice fee.
	InvoiceCodeQuotaInsufficient = "INVOICE_QUOTA_INSUFFICIENT"
	// InvoiceCodeNotFound masks absent and cross-user invoice resources.
	InvoiceCodeNotFound = "INVOICE_NOT_FOUND"
	// InvoiceCodeIdempotencyConflict identifies a reused request ID with different inputs.
	InvoiceCodeIdempotencyConflict = "INVOICE_IDEMPOTENCY_CONFLICT"
	// InvoiceCodeStateConflict identifies an illegal or stale invoice lifecycle transition.
	InvoiceCodeStateConflict = "INVOICE_STATE_CONFLICT"
	// InvoiceCodeTopUpIneligible identifies an order that cannot be claimed for invoicing.
	InvoiceCodeTopUpIneligible = "INVOICE_TOPUP_INELIGIBLE"
	// InvoiceCodePaymentEvidenceConflict identifies immutable payment-evidence drift.
	InvoiceCodePaymentEvidenceConflict = "INVOICE_PAYMENT_EVIDENCE_CONFLICT"
	// InvoiceCodeDocumentUnavailable identifies a verified document lifecycle restriction.
	InvoiceCodeDocumentUnavailable = "INVOICE_DOCUMENT_UNAVAILABLE"
	// InvoiceCodeInternalError identifies an unexpected invoice infrastructure failure.
	InvoiceCodeInternalError = "INVOICE_INTERNAL_ERROR"

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
