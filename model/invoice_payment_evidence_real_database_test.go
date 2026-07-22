package model

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormMySQL "gorm.io/driver/mysql"
	gormPostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var invoiceEvidenceDatabaseNamePattern = regexp.MustCompile(`^newapi_invoice_pe_[0-9a-f]{8}_[0-9a-f]{12}$`)

type invoiceEvidenceRealDatabase struct {
	admin   *gorm.DB
	test    *gorm.DB
	name    string
	dialect common.DatabaseType
}

type postgresInvoiceEvidenceIndexColumn struct {
	IndexName  string `gorm:"column:index_name"`
	ColumnName string `gorm:"column:column_name"`
	KeyOrder   int    `gorm:"column:key_order"`
}

func invoiceEvidenceRealDatabaseEnv(t *testing.T, dsnEnv string, databaseEnv string) (string, string, bool) {
	t.Helper()
	dsn, name := os.Getenv(dsnEnv), os.Getenv(databaseEnv)
	if dsn == "" && name == "" {
		return "", "", false
	}
	require.NotEmpty(t, dsn, dsnEnv+" is required")
	require.Regexp(t, invoiceEvidenceDatabaseNamePattern, name, databaseEnv)
	return dsn, name, true
}

func openInvoiceEvidenceMySQL(t *testing.T) (*invoiceEvidenceRealDatabase, bool) {
	t.Helper()
	dsn, name, enabled := invoiceEvidenceRealDatabaseEnv(t, "TEST_MYSQL_DSN", "TEST_INVOICE_MYSQL_DATABASE")
	if !enabled {
		return nil, false
	}
	admin, err := gorm.Open(gormMySQL.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("mysql admin connection failed")
	}
	config, err := mysqlDriver.ParseDSN(dsn)
	require.NoError(t, err)
	require.Zero(t, invoiceEvidenceDatabaseCount(t, admin, common.DatabaseTypeMySQL, name))
	require.NoError(t, admin.Exec("CREATE DATABASE `"+name+"`").Error)
	config.DBName = name
	testDB, err := gorm.Open(gormMySQL.Open(config.FormatDSN()), &gorm.Config{})
	if err != nil {
		_ = admin.Exec("DROP DATABASE `" + name + "`").Error
		t.Fatal("mysql isolated database connection failed")
	}
	return &invoiceEvidenceRealDatabase{admin: admin, test: testDB, name: name, dialect: common.DatabaseTypeMySQL}, true
}

func openInvoiceEvidencePostgreSQL(t *testing.T) (*invoiceEvidenceRealDatabase, bool) {
	t.Helper()
	dsn, name, enabled := invoiceEvidenceRealDatabaseEnv(t, "TEST_POSTGRES_DSN", "TEST_INVOICE_POSTGRES_DATABASE")
	if !enabled {
		return nil, false
	}
	admin, err := gorm.Open(gormPostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("postgres admin connection failed")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	require.Zero(t, invoiceEvidenceDatabaseCount(t, admin, common.DatabaseTypePostgreSQL, name))
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+name+`" TEMPLATE template0`).Error)
	config.Database = name
	testSQL := stdlib.OpenDB(*config)
	testDB, err := gorm.Open(gormPostgres.New(gormPostgres.Config{Conn: testSQL, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + name + `"`).Error
		t.Fatal("postgres isolated database connection failed")
	}
	var currentDatabase string
	if err := testDB.Raw("SELECT current_database()").Scan(&currentDatabase).Error; err != nil || currentDatabase != name {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + name + `"`).Error
		t.Fatal("postgres isolated database binding verification failed")
	}
	return &invoiceEvidenceRealDatabase{admin: admin, test: testDB, name: name, dialect: common.DatabaseTypePostgreSQL}, true
}

func (database *invoiceEvidenceRealDatabase) install(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = database.test, database.test
	common.SetDatabaseTypes(database.dialect, database.dialect)
	initCol()
	t.Cleanup(func() {
		testSQL, err := database.test.DB()
		if err == nil {
			_ = testSQL.Close()
		}
		drop := "DROP DATABASE `" + database.name + "`"
		if database.dialect == common.DatabaseTypePostgreSQL {
			drop = `DROP DATABASE "` + database.name + `"`
		}
		require.NoError(t, database.admin.Exec(drop).Error)
		require.Zero(t, invoiceEvidenceDatabaseCount(t, database.admin, database.dialect, database.name))
		adminSQL, err := database.admin.DB()
		if err == nil {
			_ = adminSQL.Close()
		}
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		initCol()
	})
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &SubscriptionOrder{}, &SystemTask{}, &SystemTaskLock{}))
	require.NoError(t, migrateInvoicePaymentEvidenceStructures(DB))
}

func invoiceEvidenceDatabaseCount(t *testing.T, admin *gorm.DB, dialect common.DatabaseType, name string) int64 {
	t.Helper()
	var count int64
	query := "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?"
	if dialect == common.DatabaseTypePostgreSQL {
		query = "SELECT COUNT(*) FROM pg_database WHERE datname = ?"
	}
	require.NoError(t, admin.Raw(query, name).Scan(&count).Error)
	return count
}

func runInvoiceEvidenceRealDatabaseContract(t *testing.T, database *invoiceEvidenceRealDatabase) {
	t.Helper()
	database.install(t)
	assertInvoiceEvidenceRealSchema(t, database)
	runInvoiceEvidenceExpiredLockScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidencePreClaimScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceCrashRecoveryScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceStopScenario(t, SystemTaskStatusPending)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceStopScenario(t, SystemTaskStatusRunning)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceLiveFailureScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceTerminalCompletionScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceResumeScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runTopUpInvoicePaymentSourceAcceptanceMatrix(t)
}

func assertInvoiceEvidenceRealSchema(t *testing.T, database *invoiceEvidenceRealDatabase) {
	t.Helper()
	indexes, err := DB.Migrator().GetIndexes(&TopUp{})
	require.NoError(t, err)
	wanted := map[string][]string{
		"idx_topups_evidence_run":            {"payment_evidence_run_id", "id"},
		"idx_topups_invoice_eligible_window": {"user_id", "invoice_eligible", "complete_time", "id"},
		"idx_topups_invoice_application":     {"invoice_application_id"},
		"uk_topups_provider_trade_key":       {"payment_provider_trade_key"},
	}
	postgresColumns := map[string][]string{}
	if database.dialect == common.DatabaseTypePostgreSQL {
		indexNames := make([]string, 0, len(wanted))
		for name := range wanted {
			indexNames = append(indexNames, name)
		}
		postgresColumns = postgresInvoiceEvidenceIndexColumns(t, "top_ups", indexNames)
	}
	for _, index := range indexes {
		if columns, ok := wanted[index.Name()]; ok {
			actualColumns := index.Columns()
			if database.dialect == common.DatabaseTypePostgreSQL {
				actualColumns = postgresColumns[index.Name()]
			}
			assert.Equal(t, columns, actualColumns, index.Name())
			delete(wanted, index.Name())
		}
	}
	assert.Empty(t, wanted)
	itemIndexes, err := DB.Migrator().GetIndexes(&InvoicePaymentEvidenceBackfillItem{})
	require.NoError(t, err)
	custom := make([]gorm.Index, 0, len(itemIndexes))
	for _, index := range itemIndexes {
		if primary, _ := index.PrimaryKey(); !primary {
			custom = append(custom, index)
		}
	}
	require.Len(t, custom, 1)
	assert.Equal(t, "uk_invoice_evidence_items_run_topup", custom[0].Name())
	itemColumns := custom[0].Columns()
	if database.dialect == common.DatabaseTypePostgreSQL {
		itemColumns = postgresInvoiceEvidenceIndexColumns(t, "invoice_payment_evidence_backfill_items", []string{custom[0].Name()})[custom[0].Name()]
	}
	assert.Equal(t, []string{"run_id", "topup_id"}, itemColumns)
	var foreignKeys int64
	if database.dialect == common.DatabaseTypeMySQL {
		require.NoError(t, DB.Raw("SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = DATABASE() AND table_name IN (?, ?) AND constraint_type = 'FOREIGN KEY'", "invoice_payment_evidence_backfill_runs", "invoice_payment_evidence_backfill_items").Scan(&foreignKeys).Error)
	} else {
		require.NoError(t, DB.Raw("SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_catalog = current_database() AND table_name IN (?, ?) AND constraint_type = 'FOREIGN KEY'", "invoice_payment_evidence_backfill_runs", "invoice_payment_evidence_backfill_items").Scan(&foreignKeys).Error)
	}
	assert.Zero(t, foreignKeys)
}

func postgresInvoiceEvidenceIndexColumns(t *testing.T, table string, indexNames []string) map[string][]string {
	t.Helper()
	var rows []postgresInvoiceEvidenceIndexColumn
	query := `SELECT idx.relname AS index_name, att.attname AS column_name, ord.key_order
FROM pg_index AS pi
JOIN pg_class AS tbl ON tbl.oid = pi.indrelid
JOIN pg_namespace AS ns ON ns.oid = tbl.relnamespace
JOIN pg_class AS idx ON idx.oid = pi.indexrelid
JOIN LATERAL unnest(pi.indkey::smallint[]) WITH ORDINALITY AS ord(attnum, key_order) ON ord.key_order <= pi.indnatts
JOIN pg_attribute AS att ON att.attrelid = tbl.oid AND att.attnum = ord.attnum
WHERE ns.nspname = current_schema() AND tbl.relname = ? AND idx.relname IN ?
ORDER BY idx.relname, ord.key_order`
	require.NoError(t, DB.Raw(query, table, indexNames).Scan(&rows).Error)
	return postgresInvoiceEvidenceIndexColumnsFromRows(rows)
}

func postgresInvoiceEvidenceIndexColumnsFromRows(rows []postgresInvoiceEvidenceIndexColumn) map[string][]string {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IndexName == rows[j].IndexName {
			return rows[i].KeyOrder < rows[j].KeyOrder
		}
		return rows[i].IndexName < rows[j].IndexName
	})
	columns := make(map[string][]string)
	for _, row := range rows {
		columns[row.IndexName] = append(columns[row.IndexName], row.ColumnName)
	}
	return columns
}

func resetInvoiceEvidenceRealScenario(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"invoice_payment_evidence_backfill_items", "invoice_payment_evidence_backfill_runs",
		"system_task_locks", "system_tasks", "top_ups", "subscription_orders",
	} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error, table)
	}
}

func seedRealInvoiceEvidenceRun(t *testing.T, status SystemTaskStatus, runnerID string) (*InvoicePaymentEvidenceBackfillRun, *SystemTask) {
	t.Helper()
	run := &InvoicePaymentEvidenceBackfillRun{PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion,
		CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON, PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256,
		PreviewExclusionReasons: "{}", Status: InvoicePaymentEvidenceRunStatusApplying, Attempt: 1, ActorID: 1,
		CutoverAuditJSON: "{}", CreatedAt: common.GetTimestamp()}
	require.NoError(t, DB.Create(run).Error)
	taskID, err := GenerateSystemTaskID()
	require.NoError(t, err)
	payload, err := common.Marshal(InvoicePaymentEvidenceApplyTaskPayload{Phase: "apply", RunID: run.ID, Attempt: 1, PolicySHA256: run.PolicySHA256})
	require.NoError(t, err)
	activeKey := fmt.Sprintf("%s:%d", constant.InvoicePaymentEvidenceTaskType, run.ID)
	task := &SystemTask{TaskID: taskID, Type: constant.InvoicePaymentEvidenceTaskType, Status: status, Payload: string(payload), LockedBy: runnerID}
	if status == SystemTaskStatusRunning {
		task.ActiveKey = &activeKey
	} else {
		task.Error = "lease_expired"
	}
	require.NoError(t, DB.Create(task).Error)
	require.NoError(t, DB.Model(run).Update("active_task_id", task.TaskID).Error)
	run.ActiveTaskID = &task.TaskID
	return run, task
}

func runInvoiceEvidenceExpiredLockScenario(t *testing.T) {
	t.Helper()
	task, err := CreateSystemTask(constant.InvoicePaymentEvidenceTaskType, nil, nil)
	require.NoError(t, err)
	_, claimed, err := ClaimSystemTask(task.ID, task.Type, "real-runner-old", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&SystemTaskLock{}).Where("task_id = ?", task.TaskID).Update("locked_until", common.GetTimestamp()-1).Error)
	replacementID, err := GenerateSystemTaskID()
	require.NoError(t, err)
	replacement := &SystemTask{TaskID: replacementID, Type: task.Type, Status: SystemTaskStatusPending}
	require.NoError(t, DB.Create(replacement).Error)
	_, claimed, err = ClaimSystemTask(replacement.ID, replacement.Type, "real-runner-new", common.GetTimestamp()+60)
	require.NoError(t, err)
	assert.False(t, claimed)
	require.NoError(t, DB.First(replacement, replacement.ID).Error)
	assert.Equal(t, SystemTaskStatusPending, replacement.Status)
}

func runInvoiceEvidencePreClaimScenario(t *testing.T) {
	t.Helper()
	run, _ := seedRealInvoiceEvidenceRun(t, SystemTaskStatusFailed, "real-runner-failed")
	require.NoError(t, ReconcileInvoicePaymentEvidenceBackfills(context.Background()))
	require.NoError(t, DB.First(run, run.ID).Error)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusFailed, run.Status)
}

func runInvoiceEvidenceCrashRecoveryScenario(t *testing.T) {
	t.Helper()
	run, task := seedRealInvoiceEvidenceRun(t, SystemTaskStatusRunning, "real-runner-crash")
	require.NoError(t, DB.Create(&SystemTaskLock{Type: task.Type, TaskID: task.TaskID, LockedBy: task.LockedBy, LockedUntil: common.GetTimestamp() - 1}).Error)
	replacementID, err := GenerateSystemTaskID()
	require.NoError(t, err)
	replacement := &SystemTask{TaskID: replacementID, Type: task.Type, Status: SystemTaskStatusPending}
	require.NoError(t, DB.Create(replacement).Error)
	_, claimed, err := ClaimSystemTask(replacement.ID, replacement.Type, "real-runner-after-crash", common.GetTimestamp()+60)
	require.NoError(t, err)
	assert.False(t, claimed)
	require.NoError(t, ReconcileInvoicePaymentEvidenceBackfills(context.Background()))
	require.NoError(t, DB.First(run, run.ID).Error)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusFailed, run.Status)
}

func runInvoiceEvidenceStopScenario(t *testing.T, status SystemTaskStatus) {
	t.Helper()
	run, task, _ := createBoundInvoiceEvidenceTask(t, status, 1)
	result, err := StopInvoicePaymentEvidenceBackfill(context.Background(), run.ID)
	require.NoError(t, err)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusFailed, result.Status)
	assert.Equal(t, "operator_stopped", result.Reason)
	require.NoError(t, DB.First(&task, task.ID).Error)
	assert.Equal(t, SystemTaskStatusFailed, task.Status)
	assert.Nil(t, task.ActiveKey)
}

func runInvoiceEvidenceLiveFailureScenario(t *testing.T) {
	t.Helper()
	run, task, lock := createBoundInvoiceEvidenceTask(t, SystemTaskStatusRunning, 1)
	require.NotNil(t, lock)
	require.NoError(t, FailInvoicePaymentEvidenceApply(context.Background(), run.ID, task.TaskID, task.LockedBy, run.Attempt, "apply_failed", "apply failed"))
	require.NoError(t, DB.First(&run, run.ID).Error)
	require.NoError(t, DB.First(&task, task.ID).Error)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusFailed, run.Status)
	assert.Equal(t, SystemTaskStatusFailed, task.Status)
	assert.Nil(t, run.ActiveTaskID)
	assert.Nil(t, task.ActiveKey)
}

func runInvoiceEvidenceTerminalCompletionScenario(t *testing.T) {
	t.Helper()
	run, task, lock := createBoundInvoiceEvidenceTask(t, SystemTaskStatusRunning, 1)
	require.NotNil(t, lock)
	require.NoError(t, CompleteInvoicePaymentEvidenceApply(context.Background(), run.ID, task.TaskID, task.LockedBy, run.Attempt))
	require.NoError(t, DB.First(&run, run.ID).Error)
	require.NoError(t, DB.First(&task, task.ID).Error)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusCompleted, run.Status)
	assert.Equal(t, SystemTaskStatusSucceeded, task.Status)
	assert.Nil(t, run.ActiveTaskID)
	assert.Nil(t, task.ActiveKey)
}

func runInvoiceEvidenceResumeScenario(t *testing.T) {
	t.Helper()
	run, oldTask, lock := createBoundInvoiceEvidenceTask(t, SystemTaskStatusRunning, 1)
	require.NotNil(t, lock)
	require.NoError(t, FailInvoicePaymentEvidenceApply(context.Background(), run.ID, oldTask.TaskID, oldTask.LockedBy, run.Attempt, "apply_failed", "apply failed"))
	resumedRun, newTask, created, err := EnqueueInvoicePaymentEvidenceApply(context.Background(), run.ID, run.PolicySHA256)
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, resumedRun)
	require.NotNil(t, newTask)
	assert.Equal(t, InvoicePaymentEvidenceRunStatusApplying, resumedRun.Status)
	assert.Equal(t, int64(2), resumedRun.Attempt)
	assert.Equal(t, SystemTaskStatusPending, newTask.Status)
	require.NotNil(t, resumedRun.LastTaskID)
	assert.Equal(t, oldTask.TaskID, *resumedRun.LastTaskID)
	require.NotNil(t, resumedRun.ActiveTaskID)
	assert.Equal(t, newTask.TaskID, *resumedRun.ActiveTaskID)
}

func TestPostgresInvoiceEvidenceIndexColumnsUsePhysicalKeyOrder(t *testing.T) {
	rows := []postgresInvoiceEvidenceIndexColumn{
		{IndexName: "idx_b", ColumnName: "second", KeyOrder: 2},
		{IndexName: "idx_a", ColumnName: "only", KeyOrder: 1},
		{IndexName: "idx_b", ColumnName: "first", KeyOrder: 1},
	}
	columns := postgresInvoiceEvidenceIndexColumnsFromRows(rows)
	assert.Equal(t, []string{"only"}, columns["idx_a"])
	assert.Equal(t, []string{"first", "second"}, columns["idx_b"])
}

func TestInvoiceEvidenceRealScenarioReset(t *testing.T) {
	setupInvoiceEvidenceBatchTest(t)
	run := &InvoicePaymentEvidenceBackfillRun{PolicyVersion: "reset", CanonicalPolicyJSON: "{}", PolicySHA256: "hash",
		PreviewExclusionReasons: "{}", Status: InvoicePaymentEvidenceRunStatusPreviewed, ActorID: 1, CutoverAuditJSON: "{}", CreatedAt: 1}
	require.NoError(t, DB.Create(run).Error)
	require.NoError(t, DB.Create(&InvoicePaymentEvidenceBackfillItem{RunID: run.ID, TopUpID: 1, ExpectedAmountMinor: 1, SourceFingerprint: "fingerprint", CreatedAt: 1}).Error)
	task := &SystemTask{TaskID: "reset-task", Type: constant.InvoicePaymentEvidenceTaskType, Status: SystemTaskStatusRunning, LockedBy: "reset-runner"}
	require.NoError(t, DB.Create(task).Error)
	require.NoError(t, DB.Create(&SystemTaskLock{Type: task.Type, TaskID: task.TaskID, LockedBy: task.LockedBy, LockedUntil: 1}).Error)
	require.NoError(t, DB.Create(&TopUp{TradeNo: "reset-topup"}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{TradeNo: "reset-subscription"}).Error)

	resetInvoiceEvidenceRealScenario(t)
	for _, table := range []string{"invoice_payment_evidence_backfill_items", "invoice_payment_evidence_backfill_runs", "system_task_locks", "system_tasks", "top_ups", "subscription_orders"} {
		var count int64
		require.NoError(t, DB.Table(table).Count(&count).Error)
		assert.Zero(t, count, table)
	}
}

func TestInvoiceEvidenceExtendedTransitionScenariosSQLite(t *testing.T) {
	db := setupInvoiceEvidenceFileSQLite(t)
	require.NoError(t, db.AutoMigrate(&TopUp{}, &SubscriptionOrder{}))

	runInvoiceEvidenceStopScenario(t, SystemTaskStatusPending)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceStopScenario(t, SystemTaskStatusRunning)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceLiveFailureScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceTerminalCompletionScenario(t)
	resetInvoiceEvidenceRealScenario(t)
	runInvoiceEvidenceResumeScenario(t)
}

func TestInvoicePaymentEvidenceMySQL(t *testing.T) {
	database, enabled := openInvoiceEvidenceMySQL(t)
	if enabled {
		runInvoiceEvidenceRealDatabaseContract(t, database)
	}
}

func TestInvoicePaymentEvidenceMySQLResidualDatabaseCountZero(t *testing.T) {
	dsn, name, enabled := invoiceEvidenceRealDatabaseEnv(t, "TEST_MYSQL_DSN", "TEST_INVOICE_MYSQL_DATABASE")
	if !enabled {
		return
	}
	admin, err := gorm.Open(gormMySQL.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("mysql admin connection failed")
	}
	assert.Zero(t, invoiceEvidenceDatabaseCount(t, admin, common.DatabaseTypeMySQL, name))
}

func TestInvoicePaymentEvidencePostgreSQL(t *testing.T) {
	database, enabled := openInvoiceEvidencePostgreSQL(t)
	if enabled {
		runInvoiceEvidenceRealDatabaseContract(t, database)
	}
}

func TestInvoicePaymentEvidencePostgresResidualDatabaseCountZero(t *testing.T) {
	dsn, name, enabled := invoiceEvidenceRealDatabaseEnv(t, "TEST_POSTGRES_DSN", "TEST_INVOICE_POSTGRES_DATABASE")
	if !enabled {
		return
	}
	admin, err := gorm.Open(gormPostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("postgres admin connection failed")
	}
	assert.Zero(t, invoiceEvidenceDatabaseCount(t, admin, common.DatabaseTypePostgreSQL, name))
}
