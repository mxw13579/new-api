package dto

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
