package model

import (
	"sort"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceFeeSettlementSQLiteIndexColumn struct {
	Sequence int    `gorm:"column:seqno"`
	Name     string `gorm:"column:name"`
}

type legacyInvoiceFeeLedgerEntry struct {
	ID             int64  `gorm:"primaryKey"`
	ApplicationID  int64  `gorm:"not null;uniqueIndex:uidx_invoice_fee_app_type,priority:1;index"`
	UserID         int    `gorm:"not null;index"`
	EntryType      string `gorm:"type:varchar(16);not null;uniqueIndex:uidx_invoice_fee_app_type,priority:2"`
	Quota          int    `gorm:"not null"`
	IdempotencyKey string `gorm:"type:varchar(128);not null;uniqueIndex"`
	BalanceBefore  *int
	BalanceAfter   *int
	Status         string `gorm:"type:varchar(16);not null;index"`
	CreatedAt      int64
	AppliedAt      *int64
}

func (legacyInvoiceFeeLedgerEntry) TableName() string { return "invoice_fee_ledger_entries" }

func TestInvoiceFeeRefundSettlementSQLiteMigrationContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:invoice_fee_settlement_contract?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.AutoMigrate(&legacyInvoiceFeeLedgerEntry{}))
	require.NoError(t, db.Create(&legacyInvoiceFeeLedgerEntry{
		ApplicationID: 1, UserID: 2, EntryType: InvoiceFeeEntryTypeRefund,
		Quota: 3, IdempotencyKey: "legacy-refund", Status: InvoiceFeeEntryStatusPending, CreatedAt: 4,
	}).Error)
	require.NoError(t, db.AutoMigrate(&InvoiceFeeLedgerEntry{}))
	for _, column := range []string{"last_attempt_at", "attempt_count", "last_error"} {
		assert.Truef(t, db.Migrator().HasColumn(&InvoiceFeeLedgerEntry{}, column), "missing retry metadata column %s", column)
	}
	var migrated InvoiceFeeLedgerEntry
	require.NoError(t, db.First(&migrated, 1).Error)
	assert.Zero(t, migrated.LastAttemptAt)
	assert.Zero(t, migrated.AttemptCount)
	assert.Empty(t, migrated.LastError)

	var rows []invoiceFeeSettlementSQLiteIndexColumn
	require.NoError(t, db.Raw("PRAGMA index_info('idx_invoice_fee_refund_settlement')").Scan(&rows).Error)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Sequence < rows[j].Sequence })
	columns := make([]string, 0, len(rows))
	for _, row := range rows {
		columns = append(columns, row.Name)
	}
	assert.Equal(t, []string{"entry_type", "status", "last_attempt_at", "id"}, columns)
}
