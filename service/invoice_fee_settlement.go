package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	invoiceFeeRefundSettlementBatchSize  = 100
	invoiceFeeRefundSettlementInterval   = time.Minute
	invoiceFeeRefundSettlementApplyError = "refund_apply_failed"
	invoiceFeeRefundSettlementTaskError  = "invoice_fee_refund_settlement_failed"
)

// InvoiceFeeRefundSettlementResult summarizes one bounded pending-refund reconciliation run.
type InvoiceFeeRefundSettlementResult struct {
	Scanned      int `json:"scanned"`
	Applied      int `json:"applied"`
	StillPending int `json:"still_pending"`
	Failed       int `json:"failed"`
}

type invoiceFeeRefundApplyFunc func(applicationID int64) (bool, error)
type invoiceFeeRefundFinishFunc func(taskID, runnerID string, status model.SystemTaskStatus, result any, errorMessage string) error

type invoiceFeeRefundSettlementHandler struct {
	db     *gorm.DB
	apply  invoiceFeeRefundApplyFunc
	finish invoiceFeeRefundFinishFunc
	now    func() int64
}

var _ ScheduledSystemTaskHandler = (*invoiceFeeRefundSettlementHandler)(nil)

// NewInvoiceFeeRefundSettlementHandler creates the production fee-refund reconciliation handler.
func NewInvoiceFeeRefundSettlementHandler() SystemTaskHandler {
	return newInvoiceFeeRefundSettlementHandler(model.DB, model.ApplyPendingInvoiceFeeRefund, common.GetTimestamp)
}

func newInvoiceFeeRefundSettlementHandler(db *gorm.DB, apply invoiceFeeRefundApplyFunc, now func() int64) *invoiceFeeRefundSettlementHandler {
	return &invoiceFeeRefundSettlementHandler{db: db, apply: apply, finish: model.FinishSystemTask, now: now}
}

func (*invoiceFeeRefundSettlementHandler) Type() string {
	return model.InvoiceFeeRefundSettlementTaskType
}

func (*invoiceFeeRefundSettlementHandler) Enabled() bool { return true }

func (*invoiceFeeRefundSettlementHandler) Interval() time.Duration {
	return invoiceFeeRefundSettlementInterval
}

func (*invoiceFeeRefundSettlementHandler) NewPayload() any { return nil }

func (handler *invoiceFeeRefundSettlementHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if ctx.Err() != nil {
		return
	}
	candidates, err := model.FindPendingInvoiceFeeRefundSettlementCandidates(
		handler.db.WithContext(ctx), invoiceFeeRefundSettlementBatchSize,
	)
	result := InvoiceFeeRefundSettlementResult{Scanned: len(candidates)}
	if err != nil {
		if ctx.Err() == nil {
			handler.finishTask(ctx, task, runnerID, model.SystemTaskStatusFailed, result, invoiceFeeRefundSettlementTaskError)
		}
		return
	}

	failed := false
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}
		applied, applyErr := handler.apply(candidate.ApplicationID)
		safeError := ""
		if applyErr != nil {
			safeError = invoiceFeeRefundSettlementApplyError
		}
		metadataErr := model.AdvanceInvoiceFeeRefundSettlementAttempt(handler.db, candidate.LedgerEntryID, handler.now(), safeError)
		if applyErr != nil || metadataErr != nil {
			result.Failed++
			failed = true
		} else if applied {
			result.Applied++
		} else {
			result.StillPending++
		}
	}
	if ctx.Err() != nil {
		return
	}
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if failed {
		status = model.SystemTaskStatusFailed
		errorMessage = invoiceFeeRefundSettlementTaskError
	}
	handler.finishTask(ctx, task, runnerID, status, result, errorMessage)
}

func (handler *invoiceFeeRefundSettlementHandler) finishTask(
	ctx context.Context,
	task *model.SystemTask,
	runnerID string,
	status model.SystemTaskStatus,
	result InvoiceFeeRefundSettlementResult,
	errorMessage string,
) {
	err := handler.finish(task.TaskID, runnerID, status, result, errorMessage)
	if err == nil || errors.Is(err, model.ErrSystemTaskLockLost) {
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf(
		"invoice fee refund settlement finish persistence failed: task_id=%s", task.TaskID,
	))
}
