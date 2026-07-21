package service

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

type InvoicePaymentEvidenceError struct {
	Code        string
	SafeMessage string
}

func (e *InvoicePaymentEvidenceError) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

func InvoicePaymentEvidenceErrorCode(err error) string {
	var operationalError *InvoicePaymentEvidenceError
	if errors.As(err, &operationalError) {
		return operationalError.Code
	}
	return constant.InvoicePaymentEvidenceCodeInternalError
}

func InvoicePaymentEvidenceSafeMessage(err error) string {
	var operationalError *InvoicePaymentEvidenceError
	if errors.As(err, &operationalError) {
		return operationalError.SafeMessage
	}
	return "internal error"
}

func invoicePaymentEvidenceSafeError(code string) error {
	messages := map[string]string{
		constant.InvoicePaymentEvidenceCodeInvalidRequest:   "invalid request",
		constant.InvoicePaymentEvidenceCodeRunNotFound:      "run not found",
		constant.InvoicePaymentEvidenceCodePolicyMismatch:   "policy mismatch",
		constant.InvoicePaymentEvidenceCodeRunStateConflict: "run state conflict",
		constant.InvoicePaymentEvidenceCodeCutoverNotReady:  "cutover not ready",
		constant.InvoicePaymentEvidenceCodeInternalError:    "internal error",
	}
	return &InvoicePaymentEvidenceError{Code: code, SafeMessage: messages[code]}
}

func normalizeInvoicePaymentEvidenceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrInvoicePaymentEvidenceInvalidRequest), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeInvalidRequest)
	case errors.Is(err, model.ErrInvoicePaymentEvidenceRunNotFound):
		return invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeRunNotFound)
	case errors.Is(err, model.ErrInvoicePaymentEvidencePolicyMismatch):
		return invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodePolicyMismatch)
	case errors.Is(err, model.ErrInvoicePaymentEvidenceRunStateConflict):
		return invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeRunStateConflict)
	case errors.Is(err, model.ErrInvoicePaymentEvidenceCutoverNotReady):
		return invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeCutoverNotReady)
	default:
		return invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeInternalError)
	}
}

func validInvoicePaymentEvidenceAttestation(request dto.BackfillPreviewRequest) bool {
	shaLength := len(request.DeploymentSHA)
	return (shaLength == 40 || shaLength == 64) && regexp.MustCompile(`^[0-9a-f]+$`).MatchString(request.DeploymentSHA) &&
		request.ActiveInstanceCount > 0 && request.MatchingInstanceCount == request.ActiveInstanceCount && request.DisallowedInstanceCount == 0
}

func invoicePaymentEvidencePreviewResponse(run *model.InvoicePaymentEvidenceBackfillRun) (*dto.BackfillPreviewResponse, error) {
	if run == nil || run.PreviewedAt == nil {
		return nil, model.ErrInvoicePaymentEvidenceRunStateConflict
	}
	exclusions := map[string]int64{}
	if err := common.UnmarshalJsonStr(run.PreviewExclusionReasons, &exclusions); err != nil {
		return nil, err
	}
	return &dto.BackfillPreviewResponse{
		RunID: run.ID, PolicyVersion: run.PolicyVersion, PolicySHA256: run.PolicySHA256,
		CanonicalPolicyJSON: run.CanonicalPolicyJSON, CutoffMaxTopUpID: run.CutoffMaxTopUpID,
		CandidateCount: run.PreviewCandidateCount, AmountMinor: run.PreviewAmountMinor,
		ExclusionReasons: exclusions, Status: run.Status, CreatedAt: run.CreatedAt, PreviewedAt: *run.PreviewedAt,
	}, nil
}

func PreviewInvoicePaymentEvidenceBackfill(ctx context.Context, actorID int, request dto.BackfillPreviewRequest) (*dto.BackfillPreviewResponse, bool, error) {
	if ctx == nil || actorID <= 0 || !validInvoicePaymentEvidenceAttestation(request) {
		return nil, false, invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeInvalidRequest)
	}
	run, created, err := model.PreviewInvoicePaymentEvidenceBackfill(ctx, actorID, model.InvoicePaymentEvidencePreviewAttestation{
		DeploymentSHA: request.DeploymentSHA, ActiveInstanceCount: request.ActiveInstanceCount,
		MatchingInstanceCount: request.MatchingInstanceCount, DisallowedInstanceCount: request.DisallowedInstanceCount,
	})
	if err != nil {
		return nil, false, normalizeInvoicePaymentEvidenceError(err)
	}
	response, err := invoicePaymentEvidencePreviewResponse(run)
	if err != nil {
		return nil, false, normalizeInvoicePaymentEvidenceError(err)
	}
	return response, created, nil
}

func ApplyInvoicePaymentEvidenceBackfill(ctx context.Context, actorID int, runID int64, request dto.BackfillApplyRequest) (*dto.BackfillApplyResponse, error) {
	if ctx == nil || actorID <= 0 || runID <= 0 || strings.TrimSpace(request.ExpectedPolicySHA256) == "" {
		return nil, invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeInvalidRequest)
	}
	if err := model.ReconcileInvoicePaymentEvidenceBackfills(ctx); err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	run, task, created, err := model.EnqueueInvoicePaymentEvidenceApply(ctx, runID, request.ExpectedPolicySHA256)
	if err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	return &dto.BackfillApplyResponse{
		RunID: run.ID, TaskID: task.TaskID, Attempt: run.Attempt, Created: created,
		RunStatus: run.Status, TaskStatus: string(task.Status),
	}, nil
}

func invoicePaymentEvidenceTaskLink(taskID *string, run *model.InvoicePaymentEvidenceBackfillRun) (*dto.BackfillTaskLink, error) {
	if taskID == nil {
		return nil, nil
	}
	task, err := model.GetSystemTaskByTaskID(*taskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.Type != constant.InvoicePaymentEvidenceTaskType {
		return nil, model.ErrInvoicePaymentEvidenceRunStateConflict
	}
	var payload model.InvoicePaymentEvidenceApplyTaskPayload
	if common.UnmarshalJsonStr(task.Payload, &payload) != nil || payload.Phase != "apply" ||
		payload.RunID != run.ID || payload.PolicySHA256 != run.PolicySHA256 {
		return nil, model.ErrInvoicePaymentEvidenceRunStateConflict
	}
	return &dto.BackfillTaskLink{TaskID: task.TaskID, Status: string(task.Status), Attempt: payload.Attempt}, nil
}

func GetInvoicePaymentEvidenceBackfill(ctx context.Context, runID int64) (*dto.BackfillStatusResponse, error) {
	if err := model.ReconcileInvoicePaymentEvidenceBackfills(ctx); err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	run, err := model.GetInvoicePaymentEvidenceBackfillRun(ctx, runID)
	if err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	exclusions := map[string]int64{}
	if err := common.UnmarshalJsonStr(run.PreviewExclusionReasons, &exclusions); err != nil {
		return nil, invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeInternalError)
	}
	activeTask, err := invoicePaymentEvidenceTaskLink(run.ActiveTaskID, run)
	if err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	lastTask, err := invoicePaymentEvidenceTaskLink(run.LastTaskID, run)
	if err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	return &dto.BackfillStatusResponse{
		RunID: run.ID, PolicyVersion: run.PolicyVersion, PolicySHA256: run.PolicySHA256,
		CutoffMaxTopUpID: run.CutoffMaxTopUpID, CandidateCount: run.PreviewCandidateCount,
		AmountMinor: run.PreviewAmountMinor, ExclusionReasons: exclusions, Status: run.Status,
		CursorTopUpID: run.CursorTopUpID, Attempt: run.Attempt, ActiveTask: activeTask, LastTask: lastTask,
		CreatedAt: run.CreatedAt, PreviewedAt: run.PreviewedAt, ApplyingAt: run.ApplyingAt,
		CompletedAt: run.CompletedAt, FailedAt: run.FailedAt, LastErrorCode: run.LastErrorCode, LastErrorSafe: run.LastErrorSafe,
	}, nil
}

func StopInvoicePaymentEvidenceBackfill(ctx context.Context, actorID int, runID int64) (*dto.BackfillStopResponse, error) {
	if ctx == nil || actorID <= 0 || runID <= 0 {
		return nil, invoicePaymentEvidenceSafeError(constant.InvoicePaymentEvidenceCodeInvalidRequest)
	}
	if err := model.ReconcileInvoicePaymentEvidenceBackfills(ctx); err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	result, err := model.StopInvoicePaymentEvidenceBackfill(ctx, runID)
	if err != nil {
		return nil, normalizeInvoicePaymentEvidenceError(err)
	}
	return &dto.BackfillStopResponse{RunID: result.RunID, Status: result.Status, Attempt: result.Attempt,
		LastTaskID: result.LastTaskID, FailedAt: result.FailedAt, Reason: result.Reason}, nil
}
