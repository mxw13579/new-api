package model

import (
	"unicode/utf8"

	"gorm.io/gorm"
)

const invoiceFeeSettlementLastErrorMaxLength = 128

// InvoiceFeeRefundSettlementCandidate is the minimal durable refund obligation
// shape needed by the settlement worker.
type InvoiceFeeRefundSettlementCandidate struct {
	LedgerEntryID int64 `gorm:"column:id"`
	ApplicationID int64 `gorm:"column:application_id"`
}

// FindPendingInvoiceFeeRefundSettlementCandidates loads one stable, bounded
// page of pending refund obligations.
func FindPendingInvoiceFeeRefundSettlementCandidates(db *gorm.DB, limit int) ([]InvoiceFeeRefundSettlementCandidate, error) {
	var candidates []InvoiceFeeRefundSettlementCandidate
	if db == nil || limit <= 0 {
		return candidates, ErrInvoiceStateConflict
	}
	err := db.Model(&InvoiceFeeLedgerEntry{}).
		Select("id", "application_id").
		Where("entry_type = ? AND status = ?", InvoiceFeeEntryTypeRefund, InvoiceFeeEntryStatusPending).
		Order("last_attempt_at ASC").
		Order("id ASC").
		Limit(limit).
		Scan(&candidates).Error
	return candidates, err
}

// AdvanceInvoiceFeeRefundSettlementAttempt records scheduler progress without
// changing any financial field on the ledger obligation.
func AdvanceInvoiceFeeRefundSettlementAttempt(db *gorm.DB, ledgerEntryID, attemptedAt int64, safeError string) error {
	if db == nil || ledgerEntryID <= 0 || attemptedAt <= 0 {
		return ErrInvoiceStateConflict
	}
	if utf8.RuneCountInString(safeError) > invoiceFeeSettlementLastErrorMaxLength {
		runes := []rune(safeError)
		safeError = string(runes[:invoiceFeeSettlementLastErrorMaxLength])
	}
	updated := db.Model(&InvoiceFeeLedgerEntry{}).
		Where("id = ? AND entry_type = ?", ledgerEntryID, InvoiceFeeEntryTypeRefund).
		Updates(map[string]any{
			"last_attempt_at": attemptedAt,
			"attempt_count":   gorm.Expr("attempt_count + ?", 1),
			"last_error":      safeError,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrInvoiceStateConflict
	}
	return nil
}
