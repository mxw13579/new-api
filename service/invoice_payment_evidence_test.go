package service

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInvoicePaymentEvidenceServiceTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(
		&model.SubscriptionOrder{},
		&model.InvoicePaymentEvidenceBackfillRun{},
		&model.InvoicePaymentEvidenceBackfillItem{},
	))
	for _, table := range []string{
		"invoice_payment_evidence_backfill_items",
		"invoice_payment_evidence_backfill_runs",
		"subscription_orders", "top_ups", "system_task_locks", "system_tasks",
	} {
		require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range []string{
			"invoice_payment_evidence_backfill_items",
			"invoice_payment_evidence_backfill_runs",
			"subscription_orders", "top_ups", "system_task_locks", "system_tasks",
		} {
			model.DB.Exec("DELETE FROM " + table)
		}
	})
}

func validInvoiceEvidenceAttestation() dto.BackfillPreviewRequest {
	return dto.BackfillPreviewRequest{
		DeploymentSHA:           "0123456789abcdef0123456789abcdef01234567",
		ActiveInstanceCount:     2,
		MatchingInstanceCount:   2,
		DisallowedInstanceCount: 0,
	}
}

func insertLegacyPreviewCandidate(t *testing.T, tradeNo string, money float64) model.TopUp {
	t.Helper()
	topUp := model.TopUp{
		UserId: 501, Amount: 10, Money: money, TradeNo: tradeNo,
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		CreateTime: 10, CompleteTime: 20, Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	return topUp
}

func TestInvoiceEvidencePreviewPublishesAttestedItemsAtomically(t *testing.T) {
	setupInvoicePaymentEvidenceServiceTest(t)
	candidate := insertLegacyPreviewCandidate(t, "legacy-preview", 1.25)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 501, Amount: 10, Money: 2, TradeNo: "excluded-provider",
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderStripe,
		CompleteTime: 20, Status: common.TopUpStatusSuccess,
	}).Error)
	insertLegacyPreviewCandidate(t, "excluded-wire", math.MaxFloat64)

	response, created, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, constant.InvoicePaymentEvidencePolicyVersion, response.PolicyVersion)
	assert.Equal(t, constant.InvoicePaymentEvidencePolicySHA256, response.PolicySHA256)
	assert.Equal(t, constant.InvoicePaymentEvidenceCanonicalPolicyJSON, response.CanonicalPolicyJSON)
	assert.Equal(t, int64(1), response.CandidateCount)
	assert.Equal(t, int64(125), response.AmountMinor)
	assert.Equal(t, int64(1), response.ExclusionReasons["provider_not_epay"])
	assert.Equal(t, int64(1), response.ExclusionReasons["wire_amount_invalid"])
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusPreviewed, response.Status)

	var items []model.InvoicePaymentEvidenceBackfillItem
	require.NoError(t, model.DB.Where("run_id = ?", response.RunID).Find(&items).Error)
	require.Len(t, items, 1)
	assert.Equal(t, candidate.Id, items[0].TopUpID)
	assert.Equal(t, int64(125), items[0].ExpectedAmountMinor)
	assert.Len(t, items[0].SourceFingerprint, 64)

	var run model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, model.DB.First(&run, response.RunID).Error)
	assert.JSONEq(t, `{"schema_version":1,"deployment_sha":"0123456789abcdef0123456789abcdef01234567","active_instance_count":2,"matching_instance_count":2,"disallowed_instance_count":0,"post_preview_unversioned_success_count":0}`, run.CutoverAuditJSON)

	winner, duplicateCreated, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.NoError(t, err)
	assert.False(t, duplicateCreated)
	assert.Equal(t, response.RunID, winner.RunID)
}

func TestInvoiceEvidencePreviewRejectsAttestationAndRollsBackInvisibleRun(t *testing.T) {
	setupInvoicePaymentEvidenceServiceTest(t)
	bad := validInvoiceEvidenceAttestation()
	bad.MatchingInstanceCount = 1

	_, _, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, bad)
	require.Equal(t, constant.InvoicePaymentEvidenceCodeInvalidRequest, InvoicePaymentEvidenceErrorCode(err))

	insertLegacyPreviewCandidate(t, string([]byte{0xff}), 1)
	_, _, err = PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.Equal(t, constant.InvoicePaymentEvidenceCodeInternalError, InvoicePaymentEvidenceErrorCode(err))

	var runCount, itemCount int64
	require.NoError(t, model.DB.Model(&model.InvoicePaymentEvidenceBackfillRun{}).Count(&runCount).Error)
	require.NoError(t, model.DB.Model(&model.InvoicePaymentEvidenceBackfillItem{}).Count(&itemCount).Error)
	assert.Zero(t, runCount)
	assert.Zero(t, itemCount)
}

func TestInvoiceEvidenceApplyAtomicallyBindsOneTask(t *testing.T) {
	setupInvoicePaymentEvidenceServiceTest(t)
	insertLegacyPreviewCandidate(t, "legacy-apply", 1)
	preview, _, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.NoError(t, err)

	request := dto.BackfillApplyRequest{ExpectedPolicySHA256: constant.InvoicePaymentEvidencePolicySHA256}
	response, err := ApplyInvoicePaymentEvidenceBackfill(context.Background(), 1, preview.RunID, request)
	require.NoError(t, err)
	assert.True(t, response.Created)
	assert.Equal(t, int64(1), response.Attempt)
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusApplying, response.RunStatus)
	assert.Equal(t, string(model.SystemTaskStatusPending), response.TaskStatus)

	var run model.InvoicePaymentEvidenceBackfillRun
	require.NoError(t, model.DB.First(&run, preview.RunID).Error)
	require.NotNil(t, run.ActiveTaskID)
	assert.Equal(t, response.TaskID, *run.ActiveTaskID)
	var task model.SystemTask
	require.NoError(t, model.DB.Where("task_id = ?", response.TaskID).First(&task).Error)
	require.NotNil(t, task.ActiveKey)
	assert.Equal(t, fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, preview.RunID), *task.ActiveKey)
	assert.JSONEq(t, fmt.Sprintf(`{"phase":"apply","run_id":%d,"attempt":1,"policy_sha256":"%s"}`, preview.RunID, constant.InvoicePaymentEvidencePolicySHA256), task.Payload)

	winner, err := ApplyInvoicePaymentEvidenceBackfill(context.Background(), 1, preview.RunID, request)
	require.NoError(t, err)
	assert.False(t, winner.Created)
	assert.Equal(t, response.TaskID, winner.TaskID)
	var count int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("type = ?", constant.InvoicePaymentEvidenceTaskType).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestInvoiceEvidenceApplyRejectsInvalidPersistedCutover(t *testing.T) {
	setupInvoicePaymentEvidenceServiceTest(t)
	insertLegacyPreviewCandidate(t, "legacy-cutover", 1)
	preview, _, err := PreviewInvoicePaymentEvidenceBackfill(context.Background(), 1, validInvoiceEvidenceAttestation())
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.InvoicePaymentEvidenceBackfillRun{}).
		Where("id = ?", preview.RunID).
		Update("cutover_audit_json", `{"schema_version":1,"deployment_sha":"0123456789abcdef0123456789abcdef01234567","active_instance_count":2,"matching_instance_count":1,"disallowed_instance_count":0,"post_preview_unversioned_success_count":0}`).Error)

	_, err = ApplyInvoicePaymentEvidenceBackfill(context.Background(), 1, preview.RunID, dto.BackfillApplyRequest{
		ExpectedPolicySHA256: constant.InvoicePaymentEvidencePolicySHA256,
	})
	require.Equal(t, constant.InvoicePaymentEvidenceCodeCutoverNotReady, InvoicePaymentEvidenceErrorCode(err))

	var taskCount int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("type = ?", constant.InvoicePaymentEvidenceTaskType).Count(&taskCount).Error)
	assert.Zero(t, taskCount)
}

func TestInvoiceEvidenceStatusUsesEachTaskPersistedAttempt(t *testing.T) {
	setupInvoicePaymentEvidenceServiceTest(t)
	run := model.InvoicePaymentEvidenceBackfillRun{
		PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion, CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256, Status: model.InvoicePaymentEvidenceRunStatusFailed,
		Attempt: 2, ActorID: 1, PreviewExclusionReasons: "{}", CutoverAuditJSON: "{}",
	}
	require.NoError(t, model.DB.Create(&run).Error)
	payload, err := common.Marshal(model.InvoicePaymentEvidenceApplyTaskPayload{
		Phase: "apply", RunID: run.ID, Attempt: 1, PolicySHA256: run.PolicySHA256,
	})
	require.NoError(t, err)
	task := model.SystemTask{TaskID: "systask_prior_attempt", Type: constant.InvoicePaymentEvidenceTaskType,
		Status: model.SystemTaskStatusFailed, Payload: string(payload), State: "null", Result: "null"}
	require.NoError(t, model.DB.Create(&task).Error)
	require.NoError(t, model.DB.Model(&model.InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).Update("last_task_id", task.TaskID).Error)

	response, err := GetInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
	require.NoError(t, err)
	require.NotNil(t, response.LastTask)
	assert.Equal(t, int64(1), response.LastTask.Attempt)
}

func TestInvoiceEvidenceGetRunsDurableReconciliationFirst(t *testing.T) {
	setupInvoicePaymentEvidenceServiceTest(t)
	run := model.InvoicePaymentEvidenceBackfillRun{
		PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion, CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256, Status: model.InvoicePaymentEvidenceRunStatusApplying,
		Attempt: 1, ActorID: 1, PreviewExclusionReasons: "{}", CutoverAuditJSON: "{}",
	}
	require.NoError(t, model.DB.Create(&run).Error)
	payload, err := common.Marshal(model.InvoicePaymentEvidenceApplyTaskPayload{
		Phase: "apply", RunID: run.ID, Attempt: 1, PolicySHA256: run.PolicySHA256,
	})
	require.NoError(t, err)
	taskID := "systask_service_reconcile"
	task := model.SystemTask{TaskID: taskID, Type: constant.InvoicePaymentEvidenceTaskType,
		Status: model.SystemTaskStatusFailed, Payload: string(payload), State: "null", Result: "null", Error: "lease_expired"}
	require.NoError(t, model.DB.Create(&task).Error)
	require.NoError(t, model.DB.Model(&model.InvoicePaymentEvidenceBackfillRun{}).Where("id = ?", run.ID).Update("active_task_id", taskID).Error)

	response, err := GetInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
	require.NoError(t, err)
	assert.Equal(t, model.InvoicePaymentEvidenceRunStatusFailed, response.Status)
	assert.Nil(t, response.ActiveTask)
	require.NotNil(t, response.LastTask)
	assert.Equal(t, taskID, response.LastTask.TaskID)
}
