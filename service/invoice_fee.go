package service

import "github.com/QuantumNous/new-api/model"

// ApplyPendingInvoiceFeeRefund atomically settles one full pending refund when
// the wallet has int32 headroom. A false result is an idempotent no-op or a
// still-pending obligation; callers may safely retry it later.
func ApplyPendingInvoiceFeeRefund(applicationID int64) (bool, error) {
	return model.ApplyPendingInvoiceFeeRefund(applicationID)
}
