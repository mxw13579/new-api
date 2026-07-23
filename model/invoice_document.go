package model

const (
	// InvoicePDFContentType is the only media type accepted for private invoice documents.
	InvoicePDFContentType = "application/pdf"
	// InvoiceDocumentCleanupTaskType identifies the scheduled retention and recovery task for invoice objects.
	InvoiceDocumentCleanupTaskType = "invoice_document_cleanup"

	// InvoiceDocumentStatusUploading indicates that a document is receiving its staging object.
	InvoiceDocumentStatusUploading = "uploading"
	// InvoiceDocumentStatusValidating indicates that a promoted PDF awaits aggregate finalization.
	InvoiceDocumentStatusValidating = "validating"
	// InvoiceDocumentStatusAvailable indicates that an attested document may be served to its owner.
	InvoiceDocumentStatusAvailable = "available"
	// InvoiceDocumentStatusSuperseded indicates that a newer document version is active.
	InvoiceDocumentStatusSuperseded = "superseded"
	// InvoiceDocumentStatusUploadFailed indicates that upload or PDF validation did not complete.
	InvoiceDocumentStatusUploadFailed = "upload_failed"
	// InvoiceDocumentStatusDeleting indicates that a cleanup worker owns the current deletion lease.
	InvoiceDocumentStatusDeleting = "deleting"
	// InvoiceDocumentStatusDeleted indicates that the private object was removed successfully.
	InvoiceDocumentStatusDeleted = "deleted"
	// InvoiceDocumentStatusDeleteFailed indicates a retryable or terminal object deletion failure.
	InvoiceDocumentStatusDeleteFailed = "delete_failed"
	// InvoiceDocumentStatusMissing indicates that the expected private object no longer exists.
	InvoiceDocumentStatusMissing = "missing"

	// InvoiceDocumentRecoveryDeleteRetryable records a transient staging/final object deletion failure.
	InvoiceDocumentRecoveryDeleteRetryable = "object_delete_retryable"
	// InvoiceDocumentRecoveryDeleteTerminal records a non-retryable object deletion failure.
	InvoiceDocumentRecoveryDeleteTerminal = "object_delete_terminal"
	// InvoiceDocumentRecoveryBucketMismatch records a configured/persisted bucket identity mismatch.
	InvoiceDocumentRecoveryBucketMismatch = "bucket_mismatch"
	// InvoiceDocumentRecoveryActivationIncomplete records an active pointer without complete durable activation facts.
	InvoiceDocumentRecoveryActivationIncomplete = "activation_incomplete"
)

// InvoiceDocument tracks a private PDF object's staged promotion, attestation, retention, and deletion state.
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
	RecoveryAttempts              int     `json:"recovery_attempts" gorm:"not null"`
	LastRecoveryAt                int64   `json:"last_recovery_at" gorm:"not null"`
	LastRecoveryError             string  `json:"last_recovery_error" gorm:"type:varchar(32);not null"`
	DeletedAt                     *int64  `json:"deleted_at,omitempty"`
	CreatedAt                     int64   `json:"created_at" gorm:"not null"`
	UpdatedAt                     int64   `json:"updated_at" gorm:"not null"`
}
