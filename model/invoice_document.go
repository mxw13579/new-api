package model

const (
	InvoicePDFContentType          = "application/pdf"
	InvoiceDocumentCleanupTaskType = "invoice_document_cleanup"

	InvoiceDocumentStatusUploading    = "uploading"
	InvoiceDocumentStatusValidating   = "validating"
	InvoiceDocumentStatusAvailable    = "available"
	InvoiceDocumentStatusSuperseded   = "superseded"
	InvoiceDocumentStatusUploadFailed = "upload_failed"
	InvoiceDocumentStatusDeleting     = "deleting"
	InvoiceDocumentStatusDeleted      = "deleted"
	InvoiceDocumentStatusDeleteFailed = "delete_failed"
	InvoiceDocumentStatusMissing      = "missing"
)

type InvoiceDocument struct {
	ID                            int64   `json:"id" gorm:"primaryKey"`
	ApplicationID                 int64   `json:"application_id" gorm:"not null;index:idx_invoice_documents_application"`
	IssuanceID                    *int64  `json:"issuance_id,omitempty" gorm:"uniqueIndex:uk_invoice_documents_issuance_version"`
	Version                       *int64  `json:"version,omitempty" gorm:"uniqueIndex:uk_invoice_documents_issuance_version"`
	R2Bucket                      string  `json:"-" gorm:"type:varchar(255);not null"`
	StagingObjectKey              *string `json:"-" gorm:"type:varchar(191);uniqueIndex:uk_invoice_documents_staging_key"`
	ObjectKey                     *string `json:"-" gorm:"type:varchar(191);uniqueIndex:uk_invoice_documents_object_key"`
	ContentType                   string  `json:"content_type" gorm:"type:varchar(64);not null"`
	SizeBytes                     int64   `json:"size_bytes" gorm:"not null"`
	SHA256                        string  `json:"sha256" gorm:"type:char(64);not null"`
	Status                        string  `json:"status" gorm:"type:varchar(32);not null;index:idx_invoice_documents_status_expiry,priority:1;index:idx_invoice_documents_status_operation,priority:1"`
	OperationToken                string  `json:"-" gorm:"type:char(64);not null"`
	OperationStartedAt            int64   `json:"operation_started_at" gorm:"not null;index:idx_invoice_documents_status_operation,priority:2"`
	UploadedBy                    int     `json:"uploaded_by" gorm:"not null"`
	UploadedAt                    int64   `json:"uploaded_at" gorm:"not null"`
	PDFFactsAttested              bool    `json:"pdf_facts_attested" gorm:"not null"`
	AttestedBy                    *int    `json:"attested_by,omitempty"`
	AttestedAt                    *int64  `json:"attested_at,omitempty"`
	AttestedProfileSnapshotSHA256 string  `json:"attested_profile_snapshot_sha256" gorm:"type:char(64)"`
	AvailableAt                   *int64  `json:"available_at,omitempty"`
	RetentionDaysSnapshot         int     `json:"retention_days_snapshot" gorm:"not null"`
	ExpiresAt                     *int64  `json:"expires_at,omitempty" gorm:"index:idx_invoice_documents_status_expiry,priority:2"`
	DeleteAttempts                int     `json:"delete_attempts" gorm:"not null"`
	LastDeleteError               string  `json:"last_delete_error" gorm:"type:varchar(512)"`
	DeletedAt                     *int64  `json:"deleted_at,omitempty"`
	CreatedAt                     int64   `json:"created_at" gorm:"not null"`
	UpdatedAt                     int64   `json:"updated_at" gorm:"not null"`
}
