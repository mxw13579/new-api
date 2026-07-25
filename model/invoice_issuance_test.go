package model

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openInvoiceIssuanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&InvoiceIssuance{}, &InvoiceDocument{}))
	return db
}

func TestInvoiceIssuanceUniquenessConflictClassification(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "postgres application", err: &pgconn.PgError{Code: "23505", ConstraintName: "uk_invoice_issuances_application"}, want: true},
		{name: "postgres number", err: &pgconn.PgError{Code: "23505", ConstraintName: "uk_invoice_issuances_number"}, want: true},
		{name: "postgres unrelated", err: &pgconn.PgError{Code: "23505", ConstraintName: "uk_other"}, want: false},
		{name: "mysql application", err: &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key 'invoice_issuances.uk_invoice_issuances_application'"}, want: true},
		{name: "mysql number", err: &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry 'INV-1' for key 'uk_invoice_issuances_number'"}, want: true},
		{name: "mysql unrelated", err: &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'uk_other'"}, want: false},
		{name: "sqlite application", err: errors.New("constraint failed: UNIQUE constraint failed: invoice_issuances.application_id (2067)"), want: true},
		{name: "sqlite number", err: errors.New("constraint failed: UNIQUE constraint failed: invoice_issuances.invoice_number (2067)"), want: true},
		{name: "sqlite unrelated", err: errors.New("constraint failed: UNIQUE constraint failed: invoice_documents.object_key (2067)"), want: false},
		{name: "infrastructure", err: errors.New("connection reset"), want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, isInvoiceIssuanceUniquenessConflict(testCase.err))
		})
	}
}

func TestInvoiceIssuanceAndDocumentSchemaInvariants(t *testing.T) {
	db := openInvoiceIssuanceTestDB(t)

	requiredColumns := map[any][]string{
		&InvoiceIssuance{}: {
			"id", "application_id", "invoice_number", "invoice_code", "invoice_date",
			"face_amount_minor", "currency", "created_by", "created_at", "updated_at",
		},
		&InvoiceDocument{}: {
			"id", "application_id", "issuance_id", "version", "r2_bucket", "staging_object_key",
			"object_key", "content_type", "size_bytes", "sha256", "status", "operation_token",
			"operation_started_at", "uploaded_by", "uploaded_at", "pdf_facts_attested", "attested_by",
			"attested_at", "attested_profile_snapshot_sha256", "available_at", "retention_days_snapshot",
			"expires_at", "delete_attempts", "last_delete_error", "deleted_at", "created_at", "updated_at",
		},
	}
	for value, names := range requiredColumns {
		columns, err := db.Migrator().ColumnTypes(value)
		require.NoError(t, err)
		actual := make(map[string]struct{}, len(columns))
		for _, column := range columns {
			actual[column.Name()] = struct{}{}
		}
		for _, name := range names {
			assert.Contains(t, actual, name)
		}
	}

	for _, index := range []struct {
		value any
		name  string
	}{
		{&InvoiceIssuance{}, "uk_invoice_issuances_application"},
		{&InvoiceIssuance{}, "uk_invoice_issuances_number"},
		{&InvoiceDocument{}, "uk_invoice_documents_staging_key"},
		{&InvoiceDocument{}, "uk_invoice_documents_object_key"},
		{&InvoiceDocument{}, "uk_invoice_documents_issuance_version"},
		{&InvoiceDocument{}, "idx_invoice_documents_application"},
		{&InvoiceDocument{}, "idx_invoice_documents_status_expiry"},
		{&InvoiceDocument{}, "idx_invoice_documents_status_operation"},
	} {
		assert.Truef(t, db.Migrator().HasIndex(index.value, index.name), "missing index %s", index.name)
	}

	issuance := InvoiceIssuance{
		ApplicationID: 11, InvoiceNumber: "INV-11", InvoiceDate: 100,
		FaceAmountMinor: 1200, Currency: "CNY", CreatedBy: 7,
	}
	require.NoError(t, db.Create(&issuance).Error)
	require.Error(t, db.Create(&InvoiceIssuance{
		ApplicationID: 11, InvoiceNumber: "INV-12", InvoiceDate: 100,
		FaceAmountMinor: 1200, Currency: "CNY", CreatedBy: 7,
	}).Error)
	require.Error(t, db.Create(&InvoiceIssuance{
		ApplicationID: 12, InvoiceNumber: "INV-11", InvoiceDate: 100,
		FaceAmountMinor: 1200, Currency: "CNY", CreatedBy: 7,
	}).Error)

	version := int64(1)
	firstKey, secondKey := "tmp/invoices/a.pdf", "invoices/a.pdf"
	first := InvoiceDocument{
		ApplicationID: 11, IssuanceID: &issuance.ID, Version: &version,
		R2Bucket: "private", StagingObjectKey: &firstKey, ObjectKey: &secondKey,
		ContentType: InvoicePDFContentType, Status: InvoiceDocumentStatusAvailable,
		OperationToken: "operation-a", UploadedBy: 7,
	}
	require.NoError(t, db.Create(&first).Error)

	duplicateStaging := first
	duplicateStaging.ID = 0
	duplicateStaging.ApplicationID = 12
	duplicateStaging.IssuanceID = nil
	duplicateStaging.Version = nil
	duplicateStaging.ObjectKey = nil
	require.Error(t, db.Create(&duplicateStaging).Error)

	duplicateVersion := first
	duplicateVersion.ID = 0
	otherStaging, otherObject := "tmp/invoices/b.pdf", "invoices/b.pdf"
	duplicateVersion.StagingObjectKey = &otherStaging
	duplicateVersion.ObjectKey = &otherObject
	require.Error(t, db.Create(&duplicateVersion).Error)

	duplicateObject := first
	duplicateObject.ID = 0
	duplicateObject.IssuanceID = nil
	duplicateObject.Version = nil
	duplicateObject.StagingObjectKey = &otherStaging
	require.Error(t, db.Create(&duplicateObject).Error)
}

func TestInvoiceIssuanceFactsAreImmutable(t *testing.T) {
	db := openInvoiceIssuanceTestDB(t)
	issuance := InvoiceIssuance{
		ApplicationID: 21, InvoiceNumber: "INV-21", InvoiceCode: "CODE",
		InvoiceDate: 200, FaceAmountMinor: 2200, Currency: "CNY", CreatedBy: 8,
	}
	require.NoError(t, db.Create(&issuance).Error)

	err := db.Model(&issuance).Update("invoice_number", "MUTATED").Error
	require.ErrorIs(t, err, ErrInvoiceIssuanceConflict)

	var unchanged InvoiceIssuance
	require.NoError(t, db.First(&unchanged, issuance.ID).Error)
	assert.Equal(t, "INV-21", unchanged.InvoiceNumber)
}
