package model

import (
	"testing"

	"github.com/glebarez/sqlite"
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
