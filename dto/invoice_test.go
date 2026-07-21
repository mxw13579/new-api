package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
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

func assertJSONContract(t *testing.T, value any, expected string) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	assert.JSONEq(t, expected, string(data))
}
