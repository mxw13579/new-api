package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
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

type invoiceFeeRefundSettlementHandler struct {
	db    *gorm.DB
	apply invoiceFeeRefundApplyFunc
	now   func() int64
}

var _ ScheduledSystemTaskHandler = (*invoiceFeeRefundSettlementHandler)(nil)

// NewInvoiceFeeRefundSettlementHandler creates the production fee-refund reconciliation handler.
func NewInvoiceFeeRefundSettlementHandler() SystemTaskHandler {
	return newInvoiceFeeRefundSettlementHandler(model.DB, model.ApplyPendingInvoiceFeeRefund, common.GetTimestamp)
}

func newInvoiceFeeRefundSettlementHandler(db *gorm.DB, apply invoiceFeeRefundApplyFunc, now func() int64) *invoiceFeeRefundSettlementHandler {
	return &invoiceFeeRefundSettlementHandler{db: db, apply: apply, now: now}
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
			_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, result, invoiceFeeRefundSettlementTaskError)
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
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage)
}
