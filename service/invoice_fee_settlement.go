package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
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

func (*invoiceFeeRefundSettlementHandler) Run(_ context.Context, task *model.SystemTask, runnerID string) {
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invoice_fee_refund_settlement_unavailable")
}
