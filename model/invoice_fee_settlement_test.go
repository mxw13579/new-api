package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openInvoiceFeeSettlementModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&InvoiceFeeLedgerEntry{}))
	return db
}

func TestFindPendingInvoiceFeeRefundSettlementCandidatesUsesStableBoundedPage(t *testing.T) {
	db := openInvoiceFeeSettlementModelTestDB(t)
	var candidateSQL string
	var candidateVars []any
	require.NoError(t, db.Callback().Row().After("gorm:row").Register("capture-invoice-fee-page", func(tx *gorm.DB) {
		if tx.Statement.Table == "invoice_fee_ledger_entries" {
			candidateSQL = tx.Statement.SQL.String()
			candidateVars = append([]any(nil), tx.Statement.Vars...)
		}
	}))
	t.Cleanup(func() { db.Callback().Row().Remove("capture-invoice-fee-page") })
	entries := []InvoiceFeeLedgerEntry{
		{ApplicationID: 1, UserID: 1, EntryType: InvoiceFeeEntryTypeRefund, Quota: 1, IdempotencyKey: "refund-1", Status: InvoiceFeeEntryStatusPending, LastAttemptAt: 9},
		{ApplicationID: 2, UserID: 1, EntryType: InvoiceFeeEntryTypeRefund, Quota: 1, IdempotencyKey: "refund-2", Status: InvoiceFeeEntryStatusPending, LastAttemptAt: 0},
		{ApplicationID: 3, UserID: 1, EntryType: InvoiceFeeEntryTypeCharge, Quota: 1, IdempotencyKey: "charge-3", Status: InvoiceFeeEntryStatusPending, LastAttemptAt: 0},
		{ApplicationID: 4, UserID: 1, EntryType: InvoiceFeeEntryTypeRefund, Quota: 1, IdempotencyKey: "refund-4", Status: InvoiceFeeEntryStatusApplied, LastAttemptAt: 0},
	}
	require.NoError(t, db.Create(&entries).Error)

	candidates, err := FindPendingInvoiceFeeRefundSettlementCandidates(db, 100)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	assert.Equal(t, entries[1].ID, candidates[0].LedgerEntryID)
	assert.Equal(t, entries[1].ApplicationID, candidates[0].ApplicationID)
	assert.Equal(t, entries[0].ID, candidates[1].LedgerEntryID)
	normalizedSQL := strings.Join(strings.Fields(candidateSQL), " ")
	assert.Equal(t, "SELECT `id`,`application_id` FROM `invoice_fee_ledger_entries` WHERE entry_type = ? AND status = ? ORDER BY last_attempt_at ASC,id ASC LIMIT 100", normalizedSQL)
	assert.Equal(t, []any{InvoiceFeeEntryTypeRefund, InvoiceFeeEntryStatusPending}, candidateVars)
}

func TestAdvanceInvoiceFeeRefundSettlementAttemptOnlyUpdatesRetryMetadata(t *testing.T) {
	db := openInvoiceFeeSettlementModelTestDB(t)
	before := 8
	after := 11
	entry := InvoiceFeeLedgerEntry{
		ApplicationID: 1, UserID: 2, EntryType: InvoiceFeeEntryTypeRefund, Quota: 3,
		IdempotencyKey: "refund", BalanceBefore: &before, BalanceAfter: &after,
		Status: InvoiceFeeEntryStatusApplied, AppliedAt: ptrInt64(7),
	}
	require.NoError(t, db.Create(&entry).Error)

	require.NoError(t, AdvanceInvoiceFeeRefundSettlementAttempt(db, entry.ID, 99, "refund_apply_failed"))

	var current InvoiceFeeLedgerEntry
	require.NoError(t, db.First(&current, entry.ID).Error)
	assert.Equal(t, int64(99), current.LastAttemptAt)
	assert.Equal(t, 1, current.AttemptCount)
	assert.Equal(t, "refund_apply_failed", current.LastError)
	assert.Equal(t, InvoiceFeeEntryStatusApplied, current.Status)
	assert.Equal(t, &before, current.BalanceBefore)
	assert.Equal(t, &after, current.BalanceAfter)
	assert.Equal(t, ptrInt64(7), current.AppliedAt)
}

func ptrInt64(value int64) *int64 { return &value }
