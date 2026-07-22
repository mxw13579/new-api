package model

import (
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
