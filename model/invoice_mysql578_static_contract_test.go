package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
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

	queryShapes := map[string]string{
		"pending refund": db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&InvoiceApplication{}).
				Where("fee_status = ?", constant.InvoiceFeeStatusRefundPending).
				Order("id asc").Limit(100).Find(&[]InvoiceApplication{})
		}),
		"validating recovery": db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&InvoiceDocument{}).
				Where("status IN ? AND operation_started_at <= ?", []string{InvoiceDocumentStatusUploading, InvoiceDocumentStatusValidating}, 100).
				Order("id asc").Limit(100).Find(&[]InvoiceDocument{})
		}),
		"deleting lease": db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&InvoiceDocument{}).
				Where("status = ? AND operation_started_at <= ?", InvoiceDocumentStatusDeleting, 100).
				Order("id asc").Limit(100).Find(&[]InvoiceDocument{})
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
