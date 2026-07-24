package model

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"log"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestInvoiceMySQL578StaticCompatibility_RuntimeNotRun(t *testing.T) {
	t.Log("MySQL 5.7.8 runtime: NOT RUN; this is a non-network static migration/query-shape contract")
	db := openInvoiceMySQL578DryRun(t)
	statement := &gorm.Statement{DB: db}
	require.NoError(t, statement.Parse(&InvoiceDocument{}))

	for fieldName, expectedType := range map[string]string{
		"R2AuthorityID": "char(64)",
		"ObjectETag":    "varchar(255)",
		"ExpiresAt":     "bigint",
	} {
		field := statement.Schema.LookUpField(fieldName)
		require.NotNil(t, field)
		actualType := strings.ToLower(db.Migrator().FullDataTypeOf(field).SQL)
		assert.Equal(t, expectedType, actualType)
	}
	assertPersonalInvoiceDeclaredIndexMetadata(t, db)

	var migrationSQL bytes.Buffer
	migrationDB := openInvoiceMySQL578MigrationCapture(t, &migrationSQL)
	require.NoError(t, MigratePersonalInvoiceStructures(migrationDB))
	migrationStatements := strings.ToUpper(migrationSQL.String())
	assert.Contains(t, migrationStatements, "ALTER TABLE `INVOICE_DOCUMENTS` ADD `R2_AUTHORITY_ID` CHAR(64)")
	assert.Contains(t, migrationStatements, "ALTER TABLE `INVOICE_DOCUMENTS` ADD `OBJECT_ETAG` VARCHAR(255)")
	for _, expected := range []string{
		"CREATE TABLE `INVOICE_DOCUMENTS` (`RECOVERY_ATTEMPTS` BIGINT NOT NULL DEFAULT 0",
		"`NEXT_DELETE_ATTEMPT_AT` BIGINT",
		"CREATE TABLE `INVOICE_PROFILES`",
		"CREATE TABLE `INVOICE_APPLICATIONS`",
		"CREATE TABLE `INVOICE_ITEMS`",
		"CREATE TABLE `INVOICE_FEE_LEDGER_ENTRIES`",
		"CREATE TABLE `INVOICE_ISSUANCES`",
	} {
		assert.Contains(t, migrationStatements, expected)
	}
	for _, unsupported := range []string{" RETURNING ", "::", " SERIAL", " AUTOINCREMENT", " PRAGMA ", " JSONB"} {
		assert.NotContains(t, migrationStatements, unsupported)
	}

	queryShapes := map[string]string{
		"pending refund": db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return pendingInvoiceFeeRefundSettlementCandidatesQuery(tx, 100).
				Find(&[]InvoiceFeeRefundSettlementCandidate{})
		}),
		"validating recovery": db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return InvoiceDocumentRecoveryCandidatesQuery(tx, 100, 100).
				Find(&[]InvoiceDocument{})
		}),
		"deleting lease": db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return InvoiceDocumentCleanupCandidatesQuery(tx, 200, 100, 100).
				Find(&[]InvoiceDocument{})
		}),
	}
	for name, query := range queryShapes {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, query)
			assert.Contains(t, query, "`invoice_")
			assert.Contains(t, strings.ToUpper(query), "LIMIT 100")
			for _, unsupported := range []string{" RETURNING ", " SKIP LOCKED", " ILIKE ", "::", " FILTER (", " NULLS "} {
				assert.NotContains(t, strings.ToUpper(query), unsupported)
			}
		})
	}
}

func openInvoiceMySQL578MigrationCapture(t *testing.T, output *bytes.Buffer) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(invoiceStaticConnector{})
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	dialector := invoiceStaticMigrationDialector{Dialector: mysql.New(mysql.Config{
		Conn: sqlDB, SkipInitializeWithVersion: true, ServerVersion: "5.7.8",
	})}
	db, err := gorm.Open(dialector, &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
		Logger: logger.New(log.New(output, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	require.NoError(t, err)
	return db
}

type invoiceStaticMigrationDialector struct {
	gorm.Dialector
}

func (dialector invoiceStaticMigrationDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return invoiceStaticMigrationMigrator{Migrator: dialector.Dialector.Migrator(db)}
}

type invoiceStaticMigrationMigrator struct {
	gorm.Migrator
}

func (invoiceStaticMigrationMigrator) HasTable(any) bool          { return true }
func (invoiceStaticMigrationMigrator) HasColumn(any, string) bool { return false }

func (migrator invoiceStaticMigrationMigrator) BuildIndexOptions(options []schema.IndexOption, statement *gorm.Statement) []interface{} {
	return migrator.Migrator.(interface {
		BuildIndexOptions([]schema.IndexOption, *gorm.Statement) []interface{}
	}).BuildIndexOptions(options, statement)
}

func (migrator invoiceStaticMigrationMigrator) AutoMigrate(models ...any) error {
	for _, schemaModel := range models {
		if err := migrator.CreateTable(schemaModel); err != nil {
			return err
		}
	}
	return nil
}

func openInvoiceMySQL578DryRun(t *testing.T) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(invoiceStaticConnector{})
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn: sqlDB, SkipInitializeWithVersion: true, ServerVersion: "5.7.8",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	return db
}

type invoiceStaticConnector struct{}

func (invoiceStaticConnector) Connect(context.Context) (driver.Conn, error) {
	return invoiceStaticConnection{}, nil
}

func (invoiceStaticConnector) Driver() driver.Driver { return invoiceStaticDriver{} }

type invoiceStaticDriver struct{}

func (invoiceStaticDriver) Open(string) (driver.Conn, error) { return invoiceStaticConnection{}, nil }

type invoiceStaticConnection struct{}

func (invoiceStaticConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (invoiceStaticConnection) Close() error                        { return nil }
func (invoiceStaticConnection) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
