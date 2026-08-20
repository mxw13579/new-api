package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoicePaymentEvidenceDTOJSONContract(t *testing.T) {
	previewRequest := BackfillPreviewRequest{
		DeploymentSHA:           "0123456789012345678901234567890123456789",
		ActiveInstanceCount:     3,
		MatchingInstanceCount:   3,
		DisallowedInstanceCount: 0,
	}
	assertJSONContract(t, previewRequest, `{"deployment_sha":"0123456789012345678901234567890123456789","active_instance_count":3,"matching_instance_count":3,"disallowed_instance_count":0}`)

	previewResponse := BackfillPreviewResponse{
		RunID: 1, PolicyVersion: "legacy_backfill_v1", PolicySHA256: "sha", CanonicalPolicyJSON: "{}",
		CutoffMaxTopUpID: 2, CandidateCount: 3, AmountMinor: 4,
		ExclusionReasons: map[string]int64{"status_not_success": 5}, Status: "previewed",
		CreatedAt: 6, PreviewedAt: 7,
	}
	assertJSONContract(t, previewResponse, `{"run_id":1,"policy_version":"legacy_backfill_v1","policy_sha256":"sha","canonical_policy_json":"{}","cutoff_max_topup_id":2,"candidate_count":3,"amount_minor":4,"exclusion_reasons":{"status_not_success":5},"status":"previewed","created_at":6,"previewed_at":7}`)

	assertJSONContract(t, BackfillApplyRequest{ExpectedPolicySHA256: "sha"}, `{"expected_policy_sha256":"sha"}`)
	assertJSONContract(t, BackfillApplyResponse{RunID: 1, TaskID: "task", Attempt: 2, Created: true, RunStatus: "applying", TaskStatus: "pending"}, `{"run_id":1,"task_id":"task","attempt":2,"created":true,"run_status":"applying","task_status":"pending"}`)
	assertJSONContract(t, BackfillTaskLink{TaskID: "task", Status: "running", Attempt: 2}, `{"task_id":"task","status":"running","attempt":2}`)

	previewedAt, applyingAt, completedAt, failedAt := int64(10), int64(11), int64(12), int64(13)
	errorCode, errorSafe := "lease_expired", "lease expired"
	statusResponse := BackfillStatusResponse{
		RunID: 1, PolicyVersion: "legacy_backfill_v1", PolicySHA256: "sha", CutoffMaxTopUpID: 2,
		CandidateCount: 3, AmountMinor: 4, ExclusionReasons: map[string]int64{}, Status: "failed",
		CursorTopUpID: 5, Attempt: 2,
		ActiveTask: &BackfillTaskLink{TaskID: "active", Status: "running", Attempt: 2},
		LastTask:   &BackfillTaskLink{TaskID: "last", Status: "failed", Attempt: 1},
		CreatedAt:  6, PreviewedAt: &previewedAt, ApplyingAt: &applyingAt, CompletedAt: &completedAt,
		FailedAt: &failedAt, LastErrorCode: &errorCode, LastErrorSafe: &errorSafe,
	}
	assertJSONContract(t, statusResponse, `{"run_id":1,"policy_version":"legacy_backfill_v1","policy_sha256":"sha","cutoff_max_topup_id":2,"candidate_count":3,"amount_minor":4,"exclusion_reasons":{},"status":"failed","cursor_topup_id":5,"attempt":2,"active_task":{"task_id":"active","status":"running","attempt":2},"last_task":{"task_id":"last","status":"failed","attempt":1},"created_at":6,"previewed_at":10,"applying_at":11,"completed_at":12,"failed_at":13,"last_error_code":"lease_expired","last_error_safe":"lease expired"}`)

	assertJSONContract(t, BackfillStopRequest{}, `{}`)
	lastTaskID := "task"
	assertJSONContract(t, BackfillStopResponse{RunID: 1, Status: "failed", Attempt: 2, LastTaskID: &lastTaskID, FailedAt: 3, Reason: "operator_stopped"}, `{"run_id":1,"status":"failed","attempt":2,"last_task_id":"task","failed_at":3,"reason":"operator_stopped"}`)
}

func TestPersonalInvoiceDTOJSONContract(t *testing.T) {
	assert.Equal(t, "company", constant.InvoiceTypeCompany)
	assert.Equal(t, "submitted", constant.InvoiceApplicationStatusSubmitted)
	assert.Equal(t, "resolved_voided", constant.InvoicePaymentReviewStatusResolvedVoided)
	assert.Equal(t, "refund_pending", constant.InvoiceFeeStatusRefundPending)
	assert.Equal(t, "available", constant.InvoiceDocumentStatusAvailable)
	assert.Equal(t, "INVOICE_PAYMENT_EVIDENCE_CONFLICT", constant.InvoiceCodePaymentEvidenceConflict)

	assertJSONContract(t, InvoiceConfig{
		PersonalEnabled: true, CompanyEnabled: true, ApplicationWindowDays: 90,
		MinimumAmountMinor: 100, FeePercent: 5, QuotaPerUnit: 500000, PDFRetentionDays: 365, Currency: "CNY",
	}, `{"personal_enabled":true,"company_enabled":true,"application_window_days":90,"minimum_amount_minor":100,"fee_percent":5,"quota_per_unit":500000,"pdf_retention_days":365,"currency":"CNY"}`)

	assertJSONContract(t, CreateInvoiceProfileRequest{Type: "company", Title: "Example Ltd", TaxNumber: "TAX", IsDefault: true},
		`{"type":"company","title":"Example Ltd","tax_number":"TAX","identity_card_number":"","is_default":true}`)
	assertJSONContract(t, UpdateInvoiceProfileRequest{ID: 7, ExpectedVersion: 3, Title: "Example Ltd", TaxNumber: "TAX", IsDefault: false},
		`{"id":7,"expected_version":3,"title":"Example Ltd","tax_number":"TAX","identity_card_number":"","is_default":false}`)
	assertJSONContract(t, DeleteInvoiceProfileRequest{ID: 7, ExpectedVersion: 4}, `{"id":7,"expected_version":4}`)

	assertJSONContract(t, EligibleInvoiceOrderPage{
		Items: []EligibleInvoiceOrder{{TopUpID: 11, OrderNo: "order", PaidAmountMinor: 1200, Currency: "CNY", ProductDescription: "quota", PaidAt: 100}},
		Page:  1, PageSize: 20, Total: 1,
	}, `{"items":[{"topup_id":11,"order_no":"order","paid_amount_minor":1200,"currency":"CNY","product_description":"quota","paid_at":100}],"page":1,"page_size":20,"total":1}`)

	assertJSONContract(t, CreateInvoiceApplicationRequest{RequestID: "req", ProfileID: 7, ProfileVersion: 4, TopUpIDs: []int{11, 12}},
		`{"request_id":"req","profile_id":7,"profile_version":4,"topup_ids":[11,12]}`)

	reviewedAt, expiresAt := int64(200), int64(300)
	assertJSONContract(t, InvoiceApplicationSummary{
		ID: 21, ApplicationNo: "INV-21", Type: "company", Status: "approved", PaymentReviewStatus: "none",
		Currency: "CNY", AmountMinor: 1200, FeeQuota: 20, FeeStatus: "paid", SubmittedAt: 100,
		ReviewedAt: &reviewedAt, DocumentStatus: "available", DocumentExpiresAt: &expiresAt,
		CanCancel: false, CanDownload: true,
	}, `{"id":21,"application_no":"INV-21","type":"company","status":"approved","payment_review_status":"none","currency":"CNY","amount_minor":1200,"fee_quota":20,"fee_status":"paid","submitted_at":100,"reviewed_at":200,"cancelled_at":null,"issued_at":null,"reject_reason":"","document_status":"available","document_expires_at":300,"document_deleted_at":null,"can_cancel":false,"can_download":true}`)

	assertJSONContract(t, ReviewInvoiceApplicationRequest{Action: "approve", ExpectedStatus: "reviewing"},
		`{"action":"approve","expected_status":"reviewing"}`)
	assertJSONContract(t, RejectInvoiceApplicationRequest{ExpectedStatus: "reviewing", Reason: "invalid profile"},
		`{"expected_status":"reviewing","reason":"invalid profile"}`)
	assertJSONContract(t, InvoiceDocumentUploadRequest{
		ExpectedStatus: "approved", InvoiceNumber: "N", InvoiceCode: "C", InvoiceDate: 100,
		FaceAmountMinor: 1200, Currency: "CNY", PDFFactsAttested: true,
	}, `{"expected_status":"approved","invoice_number":"N","invoice_code":"C","invoice_date":100,"face_amount_minor":1200,"currency":"CNY","pdf_facts_attested":true}`)
}

func assertJSONContract(t *testing.T, value any, expected string) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	assert.JSONEq(t, expected, string(data))
}
