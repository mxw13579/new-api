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
}
