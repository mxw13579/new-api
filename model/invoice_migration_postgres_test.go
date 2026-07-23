package model

import (
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type personalInvoiceIndexExpectation struct {
	Columns []string
	Unique  bool
}

type personalInvoicePostgreSQLIndexColumn struct {
	TableName  string `gorm:"column:table_name"`
	IndexName  string `gorm:"column:index_name"`
	ColumnName string `gorm:"column:column_name"`
	KeyOrder   int    `gorm:"column:key_order"`
	IsUnique   bool   `gorm:"column:is_unique"`
}

var personalInvoiceDeclaredIndexes = map[string]map[string]personalInvoiceIndexExpectation{
	"invoice_profiles": {
		"idx_invoice_profiles_user_type": {Columns: []string{"user_id", "type"}},
	},
	"invoice_applications": {
		"idx_invoice_applications_application_no": {Columns: []string{"application_no"}, Unique: true},
		"uidx_invoice_app_user_request":           {Columns: []string{"user_id", "request_id"}, Unique: true},
		"idx_invoice_app_user_submitted":          {Columns: []string{"user_id", "submitted_at"}},
		"idx_invoice_app_status_submitted":        {Columns: []string{"status", "submitted_at"}},
	},
	"invoice_items": {
		"uidx_invoice_item_app_topup":      {Columns: []string{"application_id", "topup_id"}, Unique: true},
		"idx_invoice_items_application_id": {Columns: []string{"application_id"}},
		"idx_invoice_items_top_up_id":      {Columns: []string{"topup_id"}},
	},
	"invoice_fee_ledger_entries": {
		"uidx_invoice_fee_app_type":                      {Columns: []string{"application_id", "entry_type"}, Unique: true},
		"idx_invoice_fee_ledger_entries_application_id":  {Columns: []string{"application_id"}},
		"idx_invoice_fee_ledger_entries_user_id":         {Columns: []string{"user_id"}},
		"idx_invoice_fee_ledger_entries_idempotency_key": {Columns: []string{"idempotency_key"}, Unique: true},
		"idx_invoice_fee_ledger_entries_status":          {Columns: []string{"status"}},
		"idx_invoice_fee_refund_settlement":              {Columns: []string{"entry_type", "status", "last_attempt_at", "id"}},
	},
	"invoice_issuances": {
		"uk_invoice_issuances_application": {Columns: []string{"application_id"}, Unique: true},
		"uk_invoice_issuances_number":      {Columns: []string{"invoice_number"}, Unique: true},
	},
	"invoice_documents": {
		"uk_invoice_documents_issuance_version":  {Columns: []string{"issuance_id", "version"}, Unique: true},
		"uk_invoice_documents_staging_key":       {Columns: []string{"staging_object_key"}, Unique: true},
		"uk_invoice_documents_object_key":        {Columns: []string{"object_key"}, Unique: true},
		"idx_invoice_documents_application":      {Columns: []string{"application_id"}},
		"idx_invoice_documents_status_expiry":    {Columns: []string{"status", "expires_at"}},
		"idx_invoice_documents_status_operation": {Columns: []string{"status", "operation_started_at"}},
		"idx_invoice_documents_delete_retry":     {Columns: []string{"status", "delete_error_category", "next_delete_attempt_at"}},
	},
}

func TestPersonalInvoicePostgreSQLMigrationShape(t *testing.T) {
	database, enabled := openInvoiceEvidencePostgreSQL(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN and TEST_INVOICE_POSTGRES_DATABASE to run personal invoice PostgreSQL migration test")
	}
	installPersonalInvoicePostgreSQLTestDatabase(t, database)

	require.NoError(t, migratePersonalInvoiceStructures(DB))
	require.NoError(t, migratePersonalInvoiceStructures(DB))
	assertPersonalInvoicePostgreSQLSchema(t, DB)
}

func installPersonalInvoicePostgreSQLTestDatabase(t *testing.T, database *invoiceEvidenceRealDatabase) {
	t.Helper()
	require.NotNil(t, database)
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = database.test, database.test
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	initCol()
	t.Cleanup(func() {
		testSQL, err := database.test.DB()
		if err == nil {
			require.NoError(t, testSQL.Close())
		}
		require.NoError(t, database.admin.Exec(`DROP DATABASE "`+database.name+`"`).Error)
		require.Zero(t, invoiceEvidenceDatabaseCount(t, database.admin, common.DatabaseTypePostgreSQL, database.name))
		adminSQL, err := database.admin.DB()
		if err == nil {
			require.NoError(t, adminSQL.Close())
		}
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		initCol()
	})
}

func assertPersonalInvoicePostgreSQLSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NotNil(t, db)
	for table := range personalInvoiceDeclaredIndexes {
		assert.Truef(t, db.Migrator().HasTable(table), "missing personal invoice table %s", table)
	}

	tables := make([]string, 0, len(personalInvoiceDeclaredIndexes))
	indexNames := make([]string, 0)
	for table, indexes := range personalInvoiceDeclaredIndexes {
		tables = append(tables, table)
		for name := range indexes {
			indexNames = append(indexNames, name)
		}
	}
	sort.Strings(tables)
	sort.Strings(indexNames)
	var rows []personalInvoicePostgreSQLIndexColumn
	query := `SELECT tbl.relname AS table_name, idx.relname AS index_name, att.attname AS column_name,
       ord.key_order, pi.indisunique AS is_unique
FROM pg_index AS pi
JOIN pg_class AS tbl ON tbl.oid = pi.indrelid
JOIN pg_namespace AS ns ON ns.oid = tbl.relnamespace
JOIN pg_class AS idx ON idx.oid = pi.indexrelid
JOIN LATERAL unnest(pi.indkey::smallint[]) WITH ORDINALITY AS ord(attnum, key_order) ON ord.key_order <= pi.indnatts
JOIN pg_attribute AS att ON att.attrelid = tbl.oid AND att.attnum = ord.attnum
WHERE ns.nspname = current_schema() AND tbl.relname IN ? AND idx.relname IN ?
ORDER BY tbl.relname, idx.relname, ord.key_order`
	require.NoError(t, db.Raw(query, tables, indexNames).Scan(&rows).Error)

	actual := make(map[string]map[string]personalInvoiceIndexExpectation)
	for _, row := range rows {
		if actual[row.TableName] == nil {
			actual[row.TableName] = make(map[string]personalInvoiceIndexExpectation)
		}
		index := actual[row.TableName][row.IndexName]
		index.Columns = append(index.Columns, row.ColumnName)
		index.Unique = row.IsUnique
		actual[row.TableName][row.IndexName] = index
	}
	assert.Equal(t, personalInvoiceDeclaredIndexes, actual)

	type columnShape struct {
		ColumnName string `gorm:"column:column_name"`
		DataType   string `gorm:"column:data_type"`
		Nullable   string `gorm:"column:is_nullable"`
		MaxLength  *int64 `gorm:"column:character_maximum_length"`
	}
	var columns []columnShape
	require.NoError(t, db.Raw(`SELECT column_name, data_type, is_nullable, character_maximum_length
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'invoice_documents'
  AND column_name IN ('delete_error_category', 'next_delete_attempt_at')
ORDER BY column_name`).Scan(&columns).Error)
	require.Len(t, columns, 2)
	assert.Equal(t, "delete_error_category", columns[0].ColumnName)
	assert.Equal(t, "character varying", columns[0].DataType)
	assert.Equal(t, "YES", columns[0].Nullable)
	require.NotNil(t, columns[0].MaxLength)
	assert.Equal(t, int64(32), *columns[0].MaxLength)
	assert.Equal(t, columnShape{ColumnName: "next_delete_attempt_at", DataType: "bigint", Nullable: "YES"}, columns[1])
}
