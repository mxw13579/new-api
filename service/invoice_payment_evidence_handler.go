package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type invoicePaymentEvidenceApplyHandler struct{}

var _ SystemTaskHandler = invoicePaymentEvidenceApplyHandler{}
var _ SystemTaskPreClaimReconciler = invoicePaymentEvidenceApplyHandler{}

func NewInvoicePaymentEvidenceApplyHandler() SystemTaskHandler {
	return invoicePaymentEvidenceApplyHandler{}
}

func (invoicePaymentEvidenceApplyHandler) Type() string {
	return constant.InvoicePaymentEvidenceTaskType
}

func (invoicePaymentEvidenceApplyHandler) ReconcileBeforeClaim(ctx context.Context) error {
	return model.ReconcileInvoicePaymentEvidenceBackfills(ctx)
}

func (invoicePaymentEvidenceApplyHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if task == nil {
		return
	}
	var payload model.InvoicePaymentEvidenceApplyTaskPayload
	if task.DecodePayload(&payload) != nil || payload.Phase != "apply" || payload.RunID <= 0 || payload.Attempt <= 0 || payload.PolicySHA256 == "" {
		_ = model.FailInvoicePaymentEvidenceInvalidPayload(ctx, task.TaskID, runnerID)
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		batch, err := model.RunInvoicePaymentEvidenceApplyBatch(ctx, payload.RunID, task.TaskID, runnerID, payload.Attempt)
		if err != nil {
			failInvoicePaymentEvidenceApplyHandler(ctx, task, runnerID, payload, err)
			return
		}
		if batch.HasMore {
			continue
		}
		if err := model.CompleteInvoicePaymentEvidenceApply(ctx, payload.RunID, task.TaskID, runnerID, payload.Attempt); err != nil {
			failInvoicePaymentEvidenceApplyHandler(ctx, task, runnerID, payload, err)
		}
		return
	}
}

func failInvoicePaymentEvidenceApplyHandler(ctx context.Context, task *model.SystemTask, runnerID string, payload model.InvoicePaymentEvidenceApplyTaskPayload, runErr error) {
	if ctx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, model.ErrSystemTaskLockLost) {
		return
	}
	_ = model.FailInvoicePaymentEvidenceApply(ctx, payload.RunID, task.TaskID, runnerID, payload.Attempt, "apply_failed", "invoice payment evidence apply failed")
}
