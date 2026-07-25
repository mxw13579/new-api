package dto

// InvoiceConfig exposes the active invoice policy and fixed settlement currency.
type InvoiceConfig struct {
	PersonalEnabled       bool    `json:"personal_enabled"`
	CompanyEnabled        bool    `json:"company_enabled"`
	ApplicationWindowDays int     `json:"application_window_days"`
	MinimumAmountMinor    int64   `json:"minimum_amount_minor"`
	FeePercent            int     `json:"fee_percent"`
	QuotaPerUnit          float64 `json:"quota_per_unit"`
	PDFRetentionDays      int     `json:"pdf_retention_days"`
	Currency              string  `json:"currency"`
}

// InvoiceProfile represents a versioned personal or company invoicing identity.
type InvoiceProfile struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	TaxNumber string `json:"tax_number"`
	IsDefault bool   `json:"is_default"`
	Version   int64  `json:"version"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// CreateInvoiceProfileRequest carries the identity fields for a new invoice profile.
type CreateInvoiceProfileRequest struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	TaxNumber string `json:"tax_number"`
	IsDefault bool   `json:"is_default"`
}

// UpdateInvoiceProfileRequest carries an optimistic-concurrency update for an invoice profile.
type UpdateInvoiceProfileRequest struct {
	ID              int64  `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	Title           string `json:"title"`
	TaxNumber       string `json:"tax_number"`
	IsDefault       bool   `json:"is_default"`
}

// DeleteInvoiceProfileRequest identifies the profile version to remove.
type DeleteInvoiceProfileRequest struct {
	ID              int64 `json:"id"`
	ExpectedVersion int64 `json:"expected_version"`
}

// EligibleInvoiceOrder describes one fully paid order available for invoice selection.
type EligibleInvoiceOrder struct {
	TopUpID            int    `json:"topup_id"`
	OrderNo            string `json:"order_no"`
	PaidAmountMinor    int64  `json:"paid_amount_minor"`
	Currency           string `json:"currency"`
	ProductDescription string `json:"product_description"`
	PaidAt             int64  `json:"paid_at"`
}

// EligibleInvoiceOrderPage is a paginated collection of invoiceable orders.
type EligibleInvoiceOrderPage struct {
	Items    []EligibleInvoiceOrder `json:"items"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Total    int64                  `json:"total"`
}

// CreateInvoiceApplicationRequest selects a profile version and owned orders for idempotent submission.
type CreateInvoiceApplicationRequest struct {
	RequestID      string `json:"request_id"`
	ProfileID      int64  `json:"profile_id"`
	ProfileVersion int64  `json:"profile_version"`
	TopUpIDs       []int  `json:"topup_ids"`
}

// InvoiceApplicationSummary exposes invoice, fee, payment-review, and document lifecycle state.
type InvoiceApplicationSummary struct {
	ID                  int64  `json:"id"`
	ApplicationNo       string `json:"application_no"`
	Type                string `json:"type"`
	Status              string `json:"status"`
	PaymentReviewStatus string `json:"payment_review_status"`
	Currency            string `json:"currency"`
	AmountMinor         int64  `json:"amount_minor"`
	FeeQuota            int64  `json:"fee_quota"`
	FeeStatus           string `json:"fee_status"`
	SubmittedAt         int64  `json:"submitted_at"`
	ReviewedAt          *int64 `json:"reviewed_at"`
	CancelledAt         *int64 `json:"cancelled_at"`
	IssuedAt            *int64 `json:"issued_at"`
	RejectReason        string `json:"reject_reason"`
	DocumentStatus      string `json:"document_status"`
	DocumentExpiresAt   *int64 `json:"document_expires_at"`
	DocumentDeletedAt   *int64 `json:"document_deleted_at"`
	CanCancel           bool   `json:"can_cancel"`
	CanDownload         bool   `json:"can_download"`
}

// InvoiceApplicationPage is a paginated collection of invoice application summaries.
type InvoiceApplicationPage struct {
	Items    []InvoiceApplicationSummary `json:"items"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
	Total    int64                       `json:"total"`
}

// InvoiceFeeHistoryItem exposes one owner-visible fee balance transition.
type InvoiceFeeHistoryItem struct {
	ID            int64  `json:"id"`
	ApplicationID int64  `json:"application_id"`
	ApplicationNo string `json:"application_no"`
	EntryType     string `json:"entry_type"`
	FeePercent    int    `json:"fee_percent"`
	Quota         int64  `json:"quota"`
	BalanceBefore *int64 `json:"balance_before"`
	BalanceAfter  *int64 `json:"balance_after"`
	Status        string `json:"status"`
	AppliedAt     *int64 `json:"applied_at"`
}

// InvoiceFeeHistoryPage is a bounded owner-scoped ledger page.
type InvoiceFeeHistoryPage struct {
	Items    []InvoiceFeeHistoryItem `json:"items"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
	Total    int64                   `json:"total"`
}

// InvoiceProfileSnapshot preserves the buyer identity used when an application was submitted.
type InvoiceProfileSnapshot struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	TaxNumber string `json:"tax_number"`
	Version   int64  `json:"version"`
}

// InvoicePolicySnapshot preserves the business policy applied to an invoice application.
type InvoicePolicySnapshot struct {
	ApplicationWindowDays int   `json:"application_window_days"`
	MinimumAmountMinor    int64 `json:"minimum_amount_minor"`
	FeePercent            int   `json:"fee_percent"`
	FeeQuota              int64 `json:"fee_quota"`
	PDFRetentionDays      int   `json:"pdf_retention_days"`
}

// InvoiceApplicationItem preserves immutable paid-order evidence attached to an application.
type InvoiceApplicationItem struct {
	TopUpID            int    `json:"topup_id"`
	OrderNo            string `json:"order_no"`
	PaidAmountMinor    int64  `json:"paid_amount_minor"`
	Currency           string `json:"currency"`
	ProductDescription string `json:"product_description"`
	PaidAt             int64  `json:"paid_at"`
}

// InvoiceIssuanceMetadata exposes immutable tax-document issuance facts.
type InvoiceIssuanceMetadata struct {
	ID              int64  `json:"id"`
	InvoiceNumber   string `json:"invoice_number"`
	InvoiceCode     string `json:"invoice_code"`
	InvoiceDate     int64  `json:"invoice_date"`
	FaceAmountMinor int64  `json:"face_amount_minor"`
	Currency        string `json:"currency"`
}

// InvoiceDocumentMetadata exposes the active PDF lifecycle without private object coordinates.
type InvoiceDocumentMetadata struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	ExpiresAt *int64 `json:"expires_at"`
	DeletedAt *int64 `json:"deleted_at"`
}

// InvoiceApplicationDetail combines an application summary with its immutable snapshots and artifacts.
type InvoiceApplicationDetail struct {
	InvoiceApplicationSummary
	ProfileSnapshot InvoiceProfileSnapshot   `json:"profile_snapshot"`
	PolicySnapshot  InvoicePolicySnapshot    `json:"policy_snapshot"`
	Items           []InvoiceApplicationItem `json:"items"`
	Issuance        *InvoiceIssuanceMetadata `json:"issuance"`
	Document        *InvoiceDocumentMetadata `json:"document"`
}

// ReviewInvoiceApplicationRequest requests an optimistic review-state transition.
type ReviewInvoiceApplicationRequest struct {
	Action         string `json:"action"`
	ExpectedStatus string `json:"expected_status"`
}

// RejectInvoiceApplicationRequest carries the expected state and required rejection reason.
type RejectInvoiceApplicationRequest struct {
	ExpectedStatus string `json:"expected_status"`
	Reason         string `json:"reason"`
}

// InvoiceDocumentUploadRequest carries administrator-attested issuance facts for a PDF upload.
type InvoiceDocumentUploadRequest struct {
	ExpectedStatus   string `json:"expected_status"`
	InvoiceNumber    string `json:"invoice_number"`
	InvoiceCode      string `json:"invoice_code"`
	InvoiceDate      int64  `json:"invoice_date"`
	FaceAmountMinor  int64  `json:"face_amount_minor"`
	Currency         string `json:"currency"`
	PDFFactsAttested bool   `json:"pdf_facts_attested"`
}

type BackfillPreviewRequest struct {
	DeploymentSHA           string `json:"deployment_sha"`
	ActiveInstanceCount     int64  `json:"active_instance_count"`
	MatchingInstanceCount   int64  `json:"matching_instance_count"`
	DisallowedInstanceCount int64  `json:"disallowed_instance_count"`
}

type BackfillPreviewResponse struct {
	RunID               int64            `json:"run_id"`
	PolicyVersion       string           `json:"policy_version"`
	PolicySHA256        string           `json:"policy_sha256"`
	CanonicalPolicyJSON string           `json:"canonical_policy_json"`
	CutoffMaxTopUpID    int              `json:"cutoff_max_topup_id"`
	CandidateCount      int64            `json:"candidate_count"`
	AmountMinor         int64            `json:"amount_minor"`
	ExclusionReasons    map[string]int64 `json:"exclusion_reasons"`
	Status              string           `json:"status"`
	CreatedAt           int64            `json:"created_at"`
	PreviewedAt         int64            `json:"previewed_at"`
}

type BackfillApplyRequest struct {
	ExpectedPolicySHA256 string `json:"expected_policy_sha256"`
}

type BackfillApplyResponse struct {
	RunID      int64  `json:"run_id"`
	TaskID     string `json:"task_id"`
	Attempt    int64  `json:"attempt"`
	Created    bool   `json:"created"`
	RunStatus  string `json:"run_status"`
	TaskStatus string `json:"task_status"`
}

type BackfillTaskLink struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	Attempt int64  `json:"attempt"`
}

type BackfillStatusResponse struct {
	RunID            int64             `json:"run_id"`
	PolicyVersion    string            `json:"policy_version"`
	PolicySHA256     string            `json:"policy_sha256"`
	CutoffMaxTopUpID int               `json:"cutoff_max_topup_id"`
	CandidateCount   int64             `json:"candidate_count"`
	AmountMinor      int64             `json:"amount_minor"`
	ExclusionReasons map[string]int64  `json:"exclusion_reasons"`
	Status           string            `json:"status"`
	CursorTopUpID    int               `json:"cursor_topup_id"`
	Attempt          int64             `json:"attempt"`
	ActiveTask       *BackfillTaskLink `json:"active_task"`
	LastTask         *BackfillTaskLink `json:"last_task"`
	CreatedAt        int64             `json:"created_at"`
	PreviewedAt      *int64            `json:"previewed_at"`
	ApplyingAt       *int64            `json:"applying_at"`
	CompletedAt      *int64            `json:"completed_at"`
	FailedAt         *int64            `json:"failed_at"`
	LastErrorCode    *string           `json:"last_error_code"`
	LastErrorSafe    *string           `json:"last_error_safe"`
}

type BackfillStopRequest struct{}

type BackfillStopResponse struct {
	RunID      int64   `json:"run_id"`
	Status     string  `json:"status"`
	Attempt    int64   `json:"attempt"`
	LastTaskID *string `json:"last_task_id"`
	FailedAt   int64   `json:"failed_at"`
	Reason     string  `json:"reason"`
}
