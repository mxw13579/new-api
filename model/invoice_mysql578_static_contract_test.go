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
	for _, existingTable := range []string{
		"INVOICE_PROFILES", "INVOICE_APPLICATIONS", "INVOICE_ITEMS", "INVOICE_FEE_LEDGER_ENTRIES", "INVOICE_ISSUANCES", "INVOICE_DOCUMENTS",
	} {
		assert.NotContains(t, migrationStatements, "CREATE TABLE `"+existingTable+"`", "historical migration cannot recreate an existing table")
	}
	orderedMigrationOperations := []string{
		"ALTER TABLE `INVOICE_DOCUMENTS` ADD `RECOVERY_ATTEMPTS` BIGINT NOT NULL DEFAULT 0",
		"ALTER TABLE `INVOICE_DOCUMENTS` ADD `LAST_RECOVERY_AT` BIGINT NOT NULL DEFAULT 0",
		"ALTER TABLE `INVOICE_DOCUMENTS` ADD `LAST_RECOVERY_ERROR` VARCHAR(32) NOT NULL DEFAULT ''",
		"ALTER TABLE `INVOICE_DOCUMENTS` ADD `DELETE_ERROR_CATEGORY` VARCHAR(32)",
		"ALTER TABLE `INVOICE_DOCUMENTS` ADD `NEXT_DELETE_ATTEMPT_AT` BIGINT",
		"CREATE INDEX `IDX_INVOICE_DOCUMENTS_DELETE_RETRY` ON `INVOICE_DOCUMENTS`",
		"ALTER TABLE `INVOICE_DOCUMENTS` ADD `R2_AUTHORITY_ID` CHAR(64)",
		"ALTER TABLE `INVOICE_DOCUMENTS` ADD `OBJECT_ETAG` VARCHAR(255)",
	}
	previousPosition := -1
	for _, expected := range orderedMigrationOperations {
		position := strings.Index(migrationStatements, expected)
		require.Greater(t, position, previousPosition, "migration operation must be present in production order: %s", expected)
		previousPosition = position
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
	dialector := invoiceStaticMigrationDialector{
		Dialector: mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true, ServerVersion: "5.7.8"}),
		historicalSchema: &invoiceStaticHistoricalSchema{
			missingColumns: map[string]map[string]bool{
				"invoice_documents": {
					"recovery_attempts": true, "last_recovery_at": true, "last_recovery_error": true,
					"delete_error_category": true, "next_delete_attempt_at": true,
					"r2_authority_id": true, "object_etag": true,
				},
			},
			missingIndexes: map[string]bool{"idx_invoice_documents_delete_retry": true},
		},
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
		Logger: logger.New(log.New(output, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	require.NoError(t, err)
	return db
}

type invoiceStaticMigrationDialector struct {
	gorm.Dialector
	historicalSchema *invoiceStaticHistoricalSchema
}

func (dialector invoiceStaticMigrationDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return invoiceStaticMigrationMigrator{
		Migrator: dialector.Dialector.Migrator(db), db: db, historicalSchema: dialector.historicalSchema,
	}
}

type invoiceStaticMigrationMigrator struct {
	gorm.Migrator
	db               *gorm.DB
	historicalSchema *invoiceStaticHistoricalSchema
}

type invoiceStaticHistoricalSchema struct {
	missingColumns map[string]map[string]bool
	missingIndexes map[string]bool
}

func (invoiceStaticMigrationMigrator) HasTable(any) bool { return true }

func (migrator invoiceStaticMigrationMigrator) HasColumn(model any, fieldName string) bool {
	statement := &gorm.Statement{DB: migrator.db}
	if statement.Parse(model) != nil {
		return true
	}
	field := statement.Schema.LookUpField(fieldName)
	if field == nil {
		return true
	}
	return !migrator.historicalSchema.missingColumns[statement.Table][strings.ToLower(field.DBName)]
}

func (migrator invoiceStaticMigrationMigrator) AddColumn(model any, fieldName string) error {
	if err := migrator.Migrator.AddColumn(model, fieldName); err != nil {
		return err
	}
	statement := &gorm.Statement{DB: migrator.db}
	if statement.Parse(model) == nil {
		if field := statement.Schema.LookUpField(fieldName); field != nil {
			delete(migrator.historicalSchema.missingColumns[statement.Table], strings.ToLower(field.DBName))
		}
	}
	return nil
}

func (migrator invoiceStaticMigrationMigrator) BuildIndexOptions(options []schema.IndexOption, statement *gorm.Statement) []interface{} {
	return migrator.Migrator.(interface {
		BuildIndexOptions([]schema.IndexOption, *gorm.Statement) []interface{}
	}).BuildIndexOptions(options, statement)
}

func (migrator invoiceStaticMigrationMigrator) AutoMigrate(models ...any) error {
	for _, schemaModel := range models {
		statement := &gorm.Statement{DB: migrator.db}
		if err := statement.Parse(schemaModel); err != nil {
			return err
		}
		for _, field := range statement.Schema.Fields {
			if migrator.historicalSchema.missingColumns[statement.Table][strings.ToLower(field.DBName)] {
				if err := migrator.AddColumn(schemaModel, field.Name); err != nil {
					return err
				}
			}
		}
		for _, index := range statement.Schema.ParseIndexes() {
			if migrator.historicalSchema.missingIndexes[strings.ToLower(index.Name)] {
				if err := migrator.CreateIndex(schemaModel, index.Name); err != nil {
					return err
				}
				delete(migrator.historicalSchema.missingIndexes, strings.ToLower(index.Name))
			}
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
