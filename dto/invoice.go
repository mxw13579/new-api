package dto

type InvoiceConfig struct {
	PersonalEnabled       bool   `json:"personal_enabled"`
	CompanyEnabled        bool   `json:"company_enabled"`
	ApplicationWindowDays int    `json:"application_window_days"`
	MinimumAmountMinor    int64  `json:"minimum_amount_minor"`
	FeeQuota              int64  `json:"fee_quota"`
	PDFRetentionDays      int    `json:"pdf_retention_days"`
	Currency              string `json:"currency"`
}

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

type CreateInvoiceProfileRequest struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	TaxNumber string `json:"tax_number"`
	IsDefault bool   `json:"is_default"`
}

type UpdateInvoiceProfileRequest struct {
	ID              int64  `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	Title           string `json:"title"`
	TaxNumber       string `json:"tax_number"`
	IsDefault       bool   `json:"is_default"`
}

type DeleteInvoiceProfileRequest struct {
	ID              int64 `json:"id"`
	ExpectedVersion int64 `json:"expected_version"`
}

type EligibleInvoiceOrder struct {
	TopUpID            int    `json:"topup_id"`
	OrderNo            string `json:"order_no"`
	PaidAmountMinor    int64  `json:"paid_amount_minor"`
	Currency           string `json:"currency"`
	ProductDescription string `json:"product_description"`
	PaidAt             int64  `json:"paid_at"`
}

type EligibleInvoiceOrderPage struct {
	Items    []EligibleInvoiceOrder `json:"items"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Total    int64                  `json:"total"`
}

type CreateInvoiceApplicationRequest struct {
	RequestID      string `json:"request_id"`
	ProfileID      int64  `json:"profile_id"`
	ProfileVersion int64  `json:"profile_version"`
	TopUpIDs       []int  `json:"topup_ids"`
}

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

type InvoiceApplicationPage struct {
	Items    []InvoiceApplicationSummary `json:"items"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
	Total    int64                       `json:"total"`
}

type InvoiceProfileSnapshot struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	TaxNumber string `json:"tax_number"`
	Version   int64  `json:"version"`
}

type InvoicePolicySnapshot struct {
	ApplicationWindowDays int   `json:"application_window_days"`
	MinimumAmountMinor    int64 `json:"minimum_amount_minor"`
	FeeQuota              int64 `json:"fee_quota"`
	PDFRetentionDays      int   `json:"pdf_retention_days"`
}

type InvoiceApplicationItem struct {
	TopUpID            int    `json:"topup_id"`
	OrderNo            string `json:"order_no"`
	PaidAmountMinor    int64  `json:"paid_amount_minor"`
	Currency           string `json:"currency"`
	ProductDescription string `json:"product_description"`
	PaidAt             int64  `json:"paid_at"`
}

type InvoiceIssuanceMetadata struct {
	ID              int64  `json:"id"`
	InvoiceNumber   string `json:"invoice_number"`
	InvoiceCode     string `json:"invoice_code"`
	InvoiceDate     int64  `json:"invoice_date"`
	FaceAmountMinor int64  `json:"face_amount_minor"`
	Currency        string `json:"currency"`
}

type InvoiceDocumentMetadata struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	ExpiresAt *int64 `json:"expires_at"`
	DeletedAt *int64 `json:"deleted_at"`
}

type InvoiceApplicationDetail struct {
	InvoiceApplicationSummary
	ProfileSnapshot InvoiceProfileSnapshot   `json:"profile_snapshot"`
	PolicySnapshot  InvoicePolicySnapshot    `json:"policy_snapshot"`
	Items           []InvoiceApplicationItem `json:"items"`
	Issuance        *InvoiceIssuanceMetadata `json:"issuance"`
	Document        *InvoiceDocumentMetadata `json:"document"`
}

type ReviewInvoiceApplicationRequest struct {
	Action         string `json:"action"`
	ExpectedStatus string `json:"expected_status"`
}

type RejectInvoiceApplicationRequest struct {
	ExpectedStatus string `json:"expected_status"`
	Reason         string `json:"reason"`
}

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
