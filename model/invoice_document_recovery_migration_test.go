package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceDocumentBeforeRecoveryMigration struct {
	ID                            int64   `gorm:"primaryKey"`
	ApplicationID                 int64   `gorm:"not null;index:idx_invoice_documents_application"`
	IssuanceID                    *int64  `gorm:"uniqueIndex:uk_invoice_documents_issuance_version"`
	Version                       *int64  `gorm:"uniqueIndex:uk_invoice_documents_issuance_version"`
	R2Bucket                      string  `gorm:"type:varchar(255);not null"`
	StagingObjectKey              *string `gorm:"type:varchar(191);uniqueIndex:uk_invoice_documents_staging_key"`
	ObjectKey                     *string `gorm:"type:varchar(191);uniqueIndex:uk_invoice_documents_object_key"`
	ContentType                   string  `gorm:"type:varchar(64);not null"`
	SizeBytes                     int64   `gorm:"not null"`
	SHA256                        string  `gorm:"type:char(64);not null"`
	Status                        string  `gorm:"type:varchar(32);not null;index:idx_invoice_documents_status_expiry,priority:1;index:idx_invoice_documents_status_operation,priority:1"`
	OperationToken                string  `gorm:"type:char(64);not null"`
	OperationStartedAt            int64   `gorm:"not null;index:idx_invoice_documents_status_operation,priority:2"`
	UploadedBy                    int     `gorm:"not null"`
	UploadedAt                    int64   `gorm:"not null"`
	PDFFactsAttested              bool    `gorm:"not null"`
	AttestedBy                    *int
	AttestedAt                    *int64
	AttestedProfileSnapshotSHA256 string `gorm:"type:char(64)"`
	AvailableAt                   *int64
	RetentionDaysSnapshot         int    `gorm:"not null"`
	ExpiresAt                     *int64 `gorm:"index:idx_invoice_documents_status_expiry,priority:2"`
	DeleteAttempts                int    `gorm:"not null"`
	LastDeleteError               string `gorm:"type:varchar(512)"`
	DeletedAt                     *int64
	CreatedAt                     int64 `gorm:"not null"`
	UpdatedAt                     int64 `gorm:"not null"`
}

func (invoiceDocumentBeforeRecoveryMigration) TableName() string { return "invoice_documents" }

func createInvoiceDocumentBeforeRecoveryMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&invoiceDocumentBeforeRecoveryMigration{}))
	require.NoError(t, db.Create(&invoiceDocumentBeforeRecoveryMigration{
		ID: 41, ApplicationID: 7, R2Bucket: "private-invoices", ContentType: InvoicePDFContentType,
		SizeBytes: 128, SHA256: "legacy-sha", Status: InvoiceDocumentStatusUploadFailed,
		OperationToken: "legacy-operation", OperationStartedAt: 100, UploadedBy: 9, UploadedAt: 100,
		RetentionDaysSnapshot: 30, CreatedAt: 100, UpdatedAt: 100,
	}).Error)
}

func assertInvoiceDocumentRecoveryUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, migratePersonalInvoiceStructures(db))

	var document InvoiceDocument
	require.NoError(t, db.First(&document, 41).Error)
	assert.Equal(t, int64(7), document.ApplicationID)
	assert.Zero(t, document.RecoveryAttempts)
	assert.Zero(t, document.LastRecoveryAt)
	assert.Empty(t, document.LastRecoveryError)
	for _, column := range []string{"recovery_attempts", "last_recovery_at", "last_recovery_error"} {
		assert.Truef(t, db.Migrator().HasColumn(&InvoiceDocument{}, column), "missing recovery column %s", column)
	}
	columnTypes, err := db.Migrator().ColumnTypes(&InvoiceDocument{})
	require.NoError(t, err)
	recoveryColumns := map[string]bool{
		"recovery_attempts":   false,
		"last_recovery_at":    false,
		"last_recovery_error": false,
	}
	for _, columnType := range columnTypes {
		if _, ok := recoveryColumns[columnType.Name()]; !ok {
			continue
		}
		recoveryColumns[columnType.Name()] = true
		if nullable, ok := columnType.Nullable(); ok {
			assert.Falsef(t, nullable, "%s must remain NOT NULL", columnType.Name())
		}
		_, hasDefault := columnType.DefaultValue()
		assert.Truef(t, hasDefault, "%s must retain an upgrade-safe default", columnType.Name())
	}
	for column, found := range recoveryColumns {
		assert.Truef(t, found, "missing recovery column metadata for %s", column)
	}

	require.NoError(t, migratePersonalInvoiceStructures(db), "invoice recovery migration must be repeatable")
	require.NoError(t, db.First(&document, 41).Error)
	assert.Equal(t, int64(7), document.ApplicationID)
}

func TestInvoiceDocumentRecoveryMigrationUpgradesSQLiteHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	createInvoiceDocumentBeforeRecoveryMigration(t, db)
	assertInvoiceDocumentRecoveryUpgrade(t, db)
}

func TestInvoiceDocumentRecoveryMigrationUpgradesIsolatedPostgreSQLHistory(t *testing.T) {
	database, enabled := openInvoiceEvidencePostgreSQL(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN and TEST_INVOICE_POSTGRES_DATABASE to run isolated PostgreSQL upgrade test")
	}
	installPersonalInvoicePostgreSQLTestDatabase(t, database)
	createInvoiceDocumentBeforeRecoveryMigration(t, DB)
	assertInvoiceDocumentRecoveryUpgrade(t, DB)
}
