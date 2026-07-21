package model

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	InvoicePaymentEvidenceRunStatusPreviewed = "previewed"
	InvoicePaymentEvidenceRunStatusApplying  = "applying"
	InvoicePaymentEvidenceRunStatusCompleted = "completed"
	InvoicePaymentEvidenceRunStatusFailed    = "failed"
	invoicePaymentEvidencePreviewBatchSize   = 500
	invoicePaymentEvidenceMaxAttempts        = 3
	invoicePaymentEvidenceRetryDelay         = 10 * time.Millisecond
)

var (
	ErrInvoicePaymentEvidenceInvalidRequest   = errors.New("invoice payment evidence invalid request")
	ErrInvoicePaymentEvidenceRunNotFound      = errors.New("invoice payment evidence run not found")
	ErrInvoicePaymentEvidencePolicyMismatch   = errors.New("invoice payment evidence policy mismatch")
	ErrInvoicePaymentEvidenceRunStateConflict = errors.New("invoice payment evidence run state conflict")
	ErrInvoicePaymentEvidenceCutoverNotReady  = errors.New("invoice payment evidence cutover not ready")
	errInvoicePaymentEvidenceUnsafeSource     = errors.New("invoice payment evidence unsafe source")
)

type InvoicePaymentEvidenceBackfillRun struct {
	ID                      int64   `json:"id" gorm:"primaryKey"`
	PolicyVersion           string  `json:"policy_version" gorm:"type:varchar(64);not null;uniqueIndex"`
	CanonicalPolicyJSON     string  `json:"canonical_policy_json" gorm:"type:text;not null"`
	PolicySHA256            string  `json:"policy_sha256" gorm:"type:char(64);not null"`
	CutoffMaxTopUpID        int     `json:"cutoff_max_topup_id" gorm:"not null"`
	PreviewCandidateCount   int64   `json:"preview_candidate_count" gorm:"not null"`
	PreviewAmountMinor      int64   `json:"preview_amount_minor" gorm:"not null"`
	PreviewExclusionReasons string  `json:"preview_exclusion_reasons" gorm:"type:text;not null"`
	Status                  string  `json:"status" gorm:"type:varchar(16);not null"`
	CursorTopUpID           int     `json:"cursor_top_up_id" gorm:"not null"`
	Attempt                 int64   `json:"attempt" gorm:"not null"`
	ActorID                 int     `json:"actor_id" gorm:"not null"`
	ActiveTaskID            *string `json:"active_task_id" gorm:"type:varchar(64)"`
	LastTaskID              *string `json:"last_task_id" gorm:"type:varchar(64)"`
	CutoverAuditJSON        string  `json:"cutover_audit_json" gorm:"type:text;not null"`
	CreatedAt               int64   `json:"created_at" gorm:"not null"`
	PreviewedAt             *int64  `json:"previewed_at"`
	ApplyingAt              *int64  `json:"applying_at"`
	CompletedAt             *int64  `json:"completed_at"`
	FailedAt                *int64  `json:"failed_at"`
	LastErrorCode           *string `json:"last_error_code" gorm:"type:varchar(64)"`
	LastErrorSafe           *string `json:"last_error_safe" gorm:"type:varchar(512)"`
}

type InvoicePaymentEvidenceBackfillItem struct {
	ID                  int64  `json:"id" gorm:"primaryKey"`
	RunID               int64  `json:"run_id" gorm:"not null;uniqueIndex:uk_invoice_evidence_items_run_topup,priority:1"`
	TopUpID             int    `json:"top_up_id" gorm:"column:topup_id;not null;uniqueIndex:uk_invoice_evidence_items_run_topup,priority:2"`
	ExpectedAmountMinor int64  `json:"expected_amount_minor" gorm:"not null"`
	SourceFingerprint   string `json:"-" gorm:"type:char(64);not null"`
	CreatedAt           int64  `json:"created_at" gorm:"not null"`
}

type InvoicePaymentEvidencePreviewAttestation struct {
	DeploymentSHA           string
	ActiveInstanceCount     int64
	MatchingInstanceCount   int64
	DisallowedInstanceCount int64
}

type InvoicePaymentEvidenceCutoverAudit struct {
	SchemaVersion                      int    `json:"schema_version"`
	DeploymentSHA                      string `json:"deployment_sha"`
	ActiveInstanceCount                int64  `json:"active_instance_count"`
	MatchingInstanceCount              int64  `json:"matching_instance_count"`
	DisallowedInstanceCount            int64  `json:"disallowed_instance_count"`
	PostPreviewUnversionedSuccessCount int64  `json:"post_preview_unversioned_success_count"`
}

type InvoicePaymentEvidenceApplyTaskPayload struct {
	Phase        string `json:"phase"`
	RunID        int64  `json:"run_id"`
	Attempt      int64  `json:"attempt"`
	PolicySHA256 string `json:"policy_sha256"`
}

func isInvoicePaymentEvidenceRetryable(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1205 || mysqlErr.Number == 1213
	}
	var postgresErr *pgconn.PgError
	if errors.As(err, &postgresErr) {
		return postgresErr.Code == "40001" || postgresErr.Code == "40P01"
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database is busy") ||
		strings.Contains(message, "sqlite_busy") || strings.Contains(message, "sqlite_locked")
}

func runInvoicePaymentEvidenceTransaction(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	if ctx == nil || db == nil || fn == nil {
		return ErrInvoicePaymentEvidenceInvalidRequest
	}
	var err error
	for attempt := 0; attempt < invoicePaymentEvidenceMaxAttempts; attempt++ {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(tx.WithContext(ctx)) })
		if err == nil || !isInvoicePaymentEvidenceRetryable(err) {
			return err
		}
		if attempt+1 < invoicePaymentEvidenceMaxAttempts {
			timer := time.NewTimer(time.Duration(attempt+1) * invoicePaymentEvidenceRetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return err
}

func validateInvoicePaymentEvidencePolicy() error {
	if len([]byte(constant.InvoicePaymentEvidenceCanonicalPolicyJSON)) != 527 {
		return ErrInvoicePaymentEvidencePolicyMismatch
	}
	hash := sha256.Sum256([]byte(constant.InvoicePaymentEvidenceCanonicalPolicyJSON))
	if hex.EncodeToString(hash[:]) != constant.InvoicePaymentEvidencePolicySHA256 {
		return ErrInvoicePaymentEvidencePolicyMismatch
	}
	return nil
}

func validatePersistedInvoicePaymentEvidencePolicy(run *InvoicePaymentEvidenceBackfillRun) error {
	if run == nil || validateInvoicePaymentEvidencePolicy() != nil ||
		run.PolicyVersion != constant.InvoicePaymentEvidencePolicyVersion ||
		run.CanonicalPolicyJSON != constant.InvoicePaymentEvidenceCanonicalPolicyJSON ||
		len([]byte(run.CanonicalPolicyJSON)) != 527 {
		return ErrInvoicePaymentEvidencePolicyMismatch
	}
	hash := sha256.Sum256([]byte(run.CanonicalPolicyJSON))
	if hex.EncodeToString(hash[:]) != run.PolicySHA256 || run.PolicySHA256 != constant.InvoicePaymentEvidencePolicySHA256 {
		return ErrInvoicePaymentEvidencePolicyMismatch
	}
	return nil
}

func invoicePaymentEvidenceFingerprint(topUp *TopUp) (string, int64, error) {
	if topUp == nil || !utf8.ValidString(topUp.TradeNo) || !utf8.ValidString(topUp.PaymentMethod) ||
		!utf8.ValidString(topUp.PaymentProvider) || !utf8.ValidString(topUp.Status) {
		return "", 0, errInvoicePaymentEvidenceUnsafeSource
	}
	minor, err := invoicePaymentEvidenceMoneyMinor(topUp.Money)
	if err != nil {
		return "", 0, err
	}
	parts := []string{strconv.Itoa(topUp.Id), topUp.TradeNo, topUp.PaymentMethod, topUp.PaymentProvider,
		topUp.Status, strconv.FormatInt(topUp.Amount, 10), strconv.FormatInt(topUp.CompleteTime, 10), strconv.FormatFloat(topUp.Money, 'f', 2, 64)}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("legacy_item_fingerprint_v1"))
	var length [4]byte
	for _, part := range parts {
		bytes := []byte(part)
		if uint64(len(bytes)) > math.MaxUint32 {
			return "", 0, errInvoicePaymentEvidenceUnsafeSource
		}
		binary.BigEndian.PutUint32(length[:], uint32(len(bytes)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write(bytes)
	}
	return hex.EncodeToString(hasher.Sum(nil)), minor, nil
}

func invoicePaymentEvidenceHasExistingEvidence(topUp *TopUp) bool {
	return (topUp.PaymentVersion != nil && *topUp.PaymentVersion != 0) || topUp.PaidAmountMinor != nil ||
		topUp.Currency != nil || topUp.InvoiceEligible != nil || topUp.InvoiceApplicationID != nil ||
		topUp.PaymentState != nil || topUp.RefundedAmountMinor != nil || topUp.ProductSnapshot != nil ||
		topUp.PaymentEvidenceSource != nil || topUp.PaymentEvidenceRunID != nil ||
		topUp.PaymentProviderTradeNo != nil || topUp.PaymentProviderTradeKey != nil
}

func invoicePaymentEvidenceCandidate(tx *gorm.DB, topUp *TopUp) (string, string, int64, error) {
	if invoicePaymentEvidenceHasExistingEvidence(topUp) {
		return "existing_evidence", "", 0, nil
	}
	if topUp.PaymentProvider != PaymentProviderEpay {
		return "provider_not_epay", "", 0, nil
	}
	if topUp.Status != common.TopUpStatusSuccess {
		return "status_not_success", "", 0, nil
	}
	var subscriptionCount int64
	if err := tx.Model(&SubscriptionOrder{}).Where("trade_no = ?", topUp.TradeNo).Count(&subscriptionCount).Error; err != nil {
		return "", "", 0, err
	}
	if subscriptionCount != 0 {
		return "subscription_trade_mirror", "", 0, nil
	}
	return invoicePaymentEvidencePostSubscriptionCandidate(topUp)
}

func invoicePaymentEvidencePostSubscriptionCandidate(topUp *TopUp) (string, string, int64, error) {
	switch {
	case strings.TrimSpace(topUp.TradeNo) == "":
		return "trade_no_blank", "", 0, nil
	case strings.TrimSpace(topUp.PaymentMethod) == "":
		return "payment_method_blank", "", 0, nil
	case topUp.CompleteTime <= 0:
		return "complete_time_nonpositive", "", 0, nil
	case topUp.Amount <= 0:
		return "amount_nonpositive", "", 0, nil
	case math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0):
		return "money_nonfinite", "", 0, nil
	case topUp.Money <= 0:
		return "money_nonpositive", "", 0, nil
	}
	fingerprint, amount, err := invoicePaymentEvidenceFingerprint(topUp)
	if err != nil {
		if errors.Is(err, errInvoicePaymentEvidenceUnsafeSource) {
			return "", "", 0, err
		}
		return "wire_amount_invalid", "", 0, nil
	}
	return "", fingerprint, amount, nil
}

func checkedAddInt64(current, delta int64) (int64, error) {
	if delta > 0 && current > math.MaxInt64-delta || delta < 0 && current < math.MinInt64-delta {
		return 0, ErrInvoicePaymentEvidenceInvalidRequest
	}
	return current + delta, nil
}

func GetInvoicePaymentEvidenceBackfillRun(ctx context.Context, runID int64) (*InvoicePaymentEvidenceBackfillRun, error) {
	if ctx == nil || runID <= 0 {
		return nil, ErrInvoicePaymentEvidenceInvalidRequest
	}
	var run InvoicePaymentEvidenceBackfillRun
	if err := DB.WithContext(ctx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoicePaymentEvidenceRunNotFound
		}
		return nil, err
	}
	return &run, nil
}
