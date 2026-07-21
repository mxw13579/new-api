package model

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
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
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+name+`"`).Error)
	config.Database = name
	testDB, err := gorm.Open(gormPostgres.New(gormPostgres.Config{DSN: config.ConnString(), PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		_ = admin.Exec(`DROP DATABASE "` + name + `"`).Error
		t.Fatal("postgres isolated database connection failed")
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
	runInvoiceEvidencePreClaimScenario(t)
	runInvoiceEvidenceCrashRecoveryScenario(t)
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
	for _, index := range indexes {
		if columns, ok := wanted[index.Name()]; ok {
			assert.Equal(t, columns, index.Columns(), index.Name())
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
	assert.Equal(t, []string{"run_id", "topup_id"}, custom[0].Columns())
	var foreignKeys int64
	if database.dialect == common.DatabaseTypeMySQL {
		require.NoError(t, DB.Raw("SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = DATABASE() AND table_name IN (?, ?) AND constraint_type = 'FOREIGN KEY'", "invoice_payment_evidence_backfill_runs", "invoice_payment_evidence_backfill_items").Scan(&foreignKeys).Error)
	} else {
		require.NoError(t, DB.Raw("SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_catalog = current_database() AND table_name IN (?, ?) AND constraint_type = 'FOREIGN KEY'", "invoice_payment_evidence_backfill_runs", "invoice_payment_evidence_backfill_items").Scan(&foreignKeys).Error)
	}
	assert.Zero(t, foreignKeys)
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
