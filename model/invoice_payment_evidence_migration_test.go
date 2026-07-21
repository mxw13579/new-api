package model

import (
	"sort"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type sqliteInvoiceEvidenceIndex struct {
	Name   string
	Unique int
}

type sqliteInvoiceEvidenceIndexColumn struct {
	Seqno int
	Name  string
}

type sqliteInvoiceEvidenceTableColumn struct {
	Name       string
	NotNull    int     `gorm:"column:notnull"`
	DefaultVal *string `gorm:"column:dflt_value"`
}

func invoiceEvidenceColumnNames(t *testing.T, db *gorm.DB, value any) map[string]gorm.ColumnType {
	t.Helper()
	columns, err := db.Migrator().ColumnTypes(value)
	require.NoError(t, err)
	result := make(map[string]gorm.ColumnType, len(columns))
	for _, column := range columns {
		result[column.Name()] = column
	}
	return result
}

func sqliteInvoiceEvidenceIndexColumns(t *testing.T, name string) []string {
	t.Helper()
	var rows []sqliteInvoiceEvidenceIndexColumn
	require.NoError(t, DB.Raw("PRAGMA index_info('"+name+"')").Scan(&rows).Error)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Seqno < rows[j].Seqno })
	columns := make([]string, 0, len(rows))
	for _, row := range rows {
		columns = append(columns, row.Name)
	}
	return columns
}

func sqliteInvoiceEvidenceTableColumns(t *testing.T, table string) map[string]sqliteInvoiceEvidenceTableColumn {
	t.Helper()
	var rows []sqliteInvoiceEvidenceTableColumn
	require.NoError(t, DB.Raw("PRAGMA table_info('"+table+"')").Scan(&rows).Error)
	columns := make(map[string]sqliteInvoiceEvidenceTableColumn, len(rows))
	for _, row := range rows {
		columns[row.Name] = row
	}
	return columns
}

func TestLegacySQLiteTopUpsMigrationAddsProviderTradeKeyIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:legacy_topups_migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.Exec(`CREATE TABLE top_ups (
		id integer PRIMARY KEY AUTOINCREMENT,
		trade_no varchar(255) UNIQUE
	)`).Error)
	require.NoError(t, db.AutoMigrate(&TopUp{}))
	require.NoError(t, migrateInvoicePaymentEvidenceStructures(db))

	assert.True(t, db.Migrator().HasColumn(&TopUp{}, "payment_provider_trade_key"))
	assert.True(t, db.Migrator().HasIndex(&topUpInvoiceEvidenceIndexMigration{}, "uk_topups_provider_trade_key"))
	require.NoError(t, db.Exec("INSERT INTO top_ups (payment_provider_trade_key) VALUES (?)", "provider-trade-key").Error)
	assert.Error(t, db.Exec("INSERT INTO top_ups (payment_provider_trade_key) VALUES (?)", "provider-trade-key").Error)
}

func TestInvoicePaymentEvidenceRoadmapDAG(t *testing.T) {
	require.NoError(t, migrateInvoicePaymentEvidenceStructures(DB))

	assert.True(t, DB.Migrator().HasTable(&InvoicePaymentEvidenceBackfillRun{}))
	assert.True(t, DB.Migrator().HasTable(&InvoicePaymentEvidenceBackfillItem{}))

	topUpColumns := invoiceEvidenceColumnNames(t, DB, &TopUp{})
	topUpSQLiteColumns := sqliteInvoiceEvidenceTableColumns(t, "top_ups")
	for _, name := range []string{
		"paid_amount_minor", "currency", "invoice_eligible", "invoice_application_id", "payment_state",
		"refunded_amount_minor", "payment_version", "product_snapshot", "payment_evidence_source",
		"payment_evidence_run_id", "payment_provider_trade_no", "payment_provider_trade_key",
	} {
		column := topUpColumns[name]
		require.NotNil(t, column, name)
		assert.Zero(t, topUpSQLiteColumns[name].NotNull, name)
		assert.Nil(t, topUpSQLiteColumns[name].DefaultVal, name)
	}

	var topUpIndexes []sqliteInvoiceEvidenceIndex
	require.NoError(t, DB.Raw("PRAGMA index_list('top_ups')").Scan(&topUpIndexes).Error)
	indexUnique := make(map[string]int, len(topUpIndexes))
	for _, index := range topUpIndexes {
		indexUnique[index.Name] = index.Unique
	}
	assert.Equal(t, []string{"payment_evidence_run_id", "id"}, sqliteInvoiceEvidenceIndexColumns(t, "idx_topups_evidence_run"))
	assert.Equal(t, []string{"user_id", "invoice_eligible", "complete_time", "id"}, sqliteInvoiceEvidenceIndexColumns(t, "idx_topups_invoice_eligible_window"))
	assert.Equal(t, []string{"invoice_application_id"}, sqliteInvoiceEvidenceIndexColumns(t, "idx_topups_invoice_application"))
	assert.Equal(t, []string{"payment_provider_trade_key"}, sqliteInvoiceEvidenceIndexColumns(t, "uk_topups_provider_trade_key"))
	assert.Equal(t, 0, indexUnique["idx_topups_evidence_run"])
	assert.Equal(t, 0, indexUnique["idx_topups_invoice_eligible_window"])
	assert.Equal(t, 0, indexUnique["idx_topups_invoice_application"])
	assert.Equal(t, 1, indexUnique["uk_topups_provider_trade_key"])

	runColumns := invoiceEvidenceColumnNames(t, DB, &InvoicePaymentEvidenceBackfillRun{})
	runColumnNames := make([]string, 0, len(runColumns))
	for name := range runColumns {
		runColumnNames = append(runColumnNames, name)
	}
	assert.ElementsMatch(t, []string{
		"id", "policy_version", "canonical_policy_json", "policy_sha256", "cutoff_max_top_up_id",
		"preview_candidate_count", "preview_amount_minor", "preview_exclusion_reasons", "status", "cursor_top_up_id",
		"attempt", "actor_id", "active_task_id", "last_task_id", "cutover_audit_json", "created_at", "previewed_at",
		"applying_at", "completed_at", "failed_at", "last_error_code", "last_error_safe",
	}, runColumnNames)
	runSQLiteColumns := sqliteInvoiceEvidenceTableColumns(t, "invoice_payment_evidence_backfill_runs")
	for _, name := range []string{
		"policy_version", "canonical_policy_json", "policy_sha256", "cutoff_max_top_up_id", "preview_candidate_count",
		"preview_amount_minor", "preview_exclusion_reasons", "status", "cursor_top_up_id", "attempt", "actor_id",
		"cutover_audit_json", "created_at",
	} {
		assert.Equal(t, 1, runSQLiteColumns[name].NotNull, name)
		assert.Nil(t, runSQLiteColumns[name].DefaultVal, name)
	}
	for _, name := range []string{"active_task_id", "last_task_id", "previewed_at", "applying_at", "completed_at", "failed_at", "last_error_code", "last_error_safe"} {
		assert.Zero(t, runSQLiteColumns[name].NotNull, name)
		assert.Nil(t, runSQLiteColumns[name].DefaultVal, name)
	}

	itemColumns := invoiceEvidenceColumnNames(t, DB, &InvoicePaymentEvidenceBackfillItem{})
	itemSQLiteColumns := sqliteInvoiceEvidenceTableColumns(t, "invoice_payment_evidence_backfill_items")
	itemColumnNames := make([]string, 0, len(itemColumns))
	for name := range itemColumns {
		itemColumnNames = append(itemColumnNames, name)
	}
	assert.ElementsMatch(t, []string{"id", "run_id", "topup_id", "expected_amount_minor", "source_fingerprint", "created_at"}, itemColumnNames)
	assert.NotContains(t, itemColumns, "top_up_id")
	for _, name := range []string{"run_id", "topup_id", "expected_amount_minor", "source_fingerprint", "created_at"} {
		assert.Equal(t, 1, itemSQLiteColumns[name].NotNull, name)
		assert.Nil(t, itemSQLiteColumns[name].DefaultVal, name)
	}

	var itemIndexes []sqliteInvoiceEvidenceIndex
	require.NoError(t, DB.Raw("PRAGMA index_list('invoice_payment_evidence_backfill_items')").Scan(&itemIndexes).Error)
	require.Len(t, itemIndexes, 1)
	assert.Equal(t, "uk_invoice_evidence_items_run_topup", itemIndexes[0].Name)
	assert.Equal(t, 1, itemIndexes[0].Unique)
	assert.Equal(t, []string{"run_id", "topup_id"}, sqliteInvoiceEvidenceIndexColumns(t, itemIndexes[0].Name))

	var foreignKeys []struct{ Table string }
	require.NoError(t, DB.Raw("PRAGMA foreign_key_list('invoice_payment_evidence_backfill_runs')").Scan(&foreignKeys).Error)
	assert.Empty(t, foreignKeys)
	require.NoError(t, DB.Raw("PRAGMA foreign_key_list('invoice_payment_evidence_backfill_items')").Scan(&foreignKeys).Error)
	assert.Empty(t, foreignKeys)

	subscriptionColumns := invoiceEvidenceColumnNames(t, DB, &SubscriptionOrder{})
	for _, forbidden := range []string{"invoice_eligible", "payment_version", "payment_evidence_source", "invoice_application_id"} {
		assert.NotContains(t, subscriptionColumns, forbidden)
	}
}
