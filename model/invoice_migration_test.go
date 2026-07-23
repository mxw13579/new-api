package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigratePersonalInvoiceStructuresCreatesConfirmedSixTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, migratePersonalInvoiceStructures(db))

	for _, value := range []any{
		&InvoiceProfile{},
		&InvoiceApplication{},
		&InvoiceItem{},
		&InvoiceFeeLedgerEntry{},
		&InvoiceIssuance{},
		&InvoiceDocument{},
	} {
		assert.Truef(t, db.Migrator().HasTable(value), "missing table for %T", value)
	}
	assertPersonalInvoiceDeclaredIndexMetadata(t, db)
}

func TestMigratePersonalInvoiceStructuresUpgradesHistoricalDocumentTableIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&historicalInvoiceDocument{}))

	require.NoError(t, migratePersonalInvoiceStructures(db))
	require.NoError(t, migratePersonalInvoiceStructures(db))

	type sqliteColumn struct {
		Name    string `gorm:"column:name"`
		Type    string `gorm:"column:type"`
		NotNull int    `gorm:"column:notnull"`
	}
	var columns []sqliteColumn
	require.NoError(t, db.Raw(`PRAGMA table_info('invoice_documents')`).Scan(&columns).Error)
	byName := make(map[string]sqliteColumn, len(columns))
	for _, column := range columns {
		byName[column.Name] = column
	}
	category, ok := byName["delete_error_category"]
	require.True(t, ok)
	assert.Equal(t, "varchar(32)", strings.ToLower(category.Type))
	assert.Zero(t, category.NotNull)
	nextAttempt, ok := byName["next_delete_attempt_at"]
	require.True(t, ok)
	assert.Equal(t, "bigint", strings.ToLower(nextAttempt.Type))
	assert.Zero(t, nextAttempt.NotNull)

	type sqliteIndexColumn struct {
		Name string `gorm:"column:name"`
	}
	var indexColumns []sqliteIndexColumn
	require.NoError(t, db.Raw(`PRAGMA index_info('idx_invoice_documents_delete_retry')`).Scan(&indexColumns).Error)
	actual := make([]string, 0, len(indexColumns))
	for _, column := range indexColumns {
		actual = append(actual, column.Name)
	}
	assert.Equal(t, []string{"status", "delete_error_category", "next_delete_attempt_at"}, actual)
}

type historicalInvoiceDocument struct {
	ID               int64   `gorm:"primaryKey"`
	IssuanceID       *int64  `gorm:"uniqueIndex:uk_invoice_documents_issuance_version"`
	Version          *int64  `gorm:"uniqueIndex:uk_invoice_documents_issuance_version"`
	StagingObjectKey *string `gorm:"type:varchar(191);uniqueIndex:uk_invoice_documents_staging_key"`
	ObjectKey        *string `gorm:"type:varchar(191);uniqueIndex:uk_invoice_documents_object_key"`
	Status           string  `gorm:"type:varchar(32);not null"`
}

func (historicalInvoiceDocument) TableName() string { return "invoice_documents" }

func assertPersonalInvoiceDeclaredIndexMetadata(t *testing.T, db *gorm.DB) {
	t.Helper()
	models := map[string]any{
		"invoice_profiles":           &InvoiceProfile{},
		"invoice_applications":       &InvoiceApplication{},
		"invoice_items":              &InvoiceItem{},
		"invoice_fee_ledger_entries": &InvoiceFeeLedgerEntry{},
		"invoice_issuances":          &InvoiceIssuance{},
		"invoice_documents":          &InvoiceDocument{},
	}
	actual := make(map[string]map[string]personalInvoiceIndexExpectation, len(models))
	for table, value := range models {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		actual[table] = make(map[string]personalInvoiceIndexExpectation)
		for name, index := range statement.Schema.ParseIndexes() {
			columns := make([]string, 0, len(index.Fields))
			for _, field := range index.Fields {
				columns = append(columns, field.Field.DBName)
			}
			actual[table][name] = personalInvoiceIndexExpectation{Columns: columns, Unique: index.Class == "UNIQUE"}
		}
	}
	assert.Equal(t, personalInvoiceDeclaredIndexes, actual)
}
