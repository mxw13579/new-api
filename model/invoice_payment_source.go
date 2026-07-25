package model

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

// TopUpInvoicePaymentSource reserves trusted wallet top-up evidence for invoice applications.
type TopUpInvoicePaymentSource struct{}

var _ InvoicePaymentSource = (*TopUpInvoicePaymentSource)(nil)

// NewTopUpInvoicePaymentSource returns the payment-evidence adapter backed by wallet top-ups.
func NewTopUpInvoicePaymentSource() InvoicePaymentSource {
	return &TopUpInvoicePaymentSource{}
}

func validateInvoicePaymentSourceInput(tx *gorm.DB, userID int, applicationID int64, topUps []TopUpVersionExpectation) ([]TopUpVersionExpectation, error) {
	if tx == nil || userID <= 0 || applicationID <= 0 || len(topUps) == 0 {
		return nil, ErrInvoicePaymentSourceInvalidRequest
	}
	ordered := append([]TopUpVersionExpectation(nil), topUps...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].TopUpID < ordered[j].TopUpID })
	for i, topUp := range ordered {
		if topUp.TopUpID <= 0 || topUp.ExpectedPaymentVersion <= 0 || topUp.ExpectedPaymentVersion == math.MaxInt64 ||
			(i > 0 && ordered[i-1].TopUpID == topUp.TopUpID) {
			return nil, ErrInvoicePaymentSourceInvalidRequest
		}
	}
	return ordered, nil
}

func validateInvoicePaymentSourceImmutableEvidence(tx *gorm.DB, topUp *TopUp) error {
	if topUp.PaidAmountMinor == nil || *topUp.PaidAmountMinor <= 0 || topUp.Currency == nil || *topUp.Currency != constant.InvoicePaymentEvidenceCurrencyCNY ||
		topUp.ProductSnapshot == nil || strings.TrimSpace(*topUp.ProductSnapshot) == "" {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	return validateInvoicePaymentSourceEvidence(tx, topUp)
}

func validateInvoicePaymentSourceCommon(tx *gorm.DB, topUp *TopUp, userID int) error {
	if topUp.UserId != userID || topUp.Status != common.TopUpStatusSuccess || topUp.InvoiceEligible == nil || !*topUp.InvoiceEligible || topUp.CompleteTime <= 0 {
		return ErrInvoicePaymentSourceNotEligible
	}
	if topUp.PaidAmountMinor == nil || *topUp.PaidAmountMinor <= 0 || topUp.Currency == nil || *topUp.Currency != constant.InvoicePaymentEvidenceCurrencyCNY ||
		topUp.PaymentState == nil || *topUp.PaymentState != constant.InvoicePaymentStateSucceeded || topUp.RefundedAmountMinor == nil || *topUp.RefundedAmountMinor != 0 ||
		topUp.ProductSnapshot == nil || strings.TrimSpace(*topUp.ProductSnapshot) == "" {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	var subscriptionCount int64
	if err := tx.Model(&SubscriptionOrder{}).Where("trade_no = ?", topUp.TradeNo).Count(&subscriptionCount).Error; err != nil {
		return err
	}
	if subscriptionCount != 0 {
		return ErrInvoicePaymentSourceNotEligible
	}
	return nil
}

func validateInvoicePaymentSourceEvidence(tx *gorm.DB, topUp *TopUp) error {
	if topUp.PaymentEvidenceSource == nil {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	switch *topUp.PaymentEvidenceSource {
	case constant.InvoicePaymentEvidenceSourceTrustedCallback:
		return validateTrustedInvoicePaymentSourceEvidence(topUp)
	case constant.InvoicePaymentEvidenceSourceLegacyBackfill:
		return validateLegacyInvoicePaymentSourceEvidence(tx, topUp)
	case constant.InvoicePaymentEvidenceSourceAdminManualCompletion:
		if topUp.PaymentEvidenceRunID != nil || topUp.PaymentProviderTradeNo != nil || topUp.PaymentProviderTradeKey != nil {
			return ErrInvoicePaymentSourceEvidenceConflict
		}
		return nil
	default:
		return ErrInvoicePaymentSourceEvidenceConflict
	}
}

func validateTrustedInvoicePaymentSourceEvidence(topUp *TopUp) error {
	if topUp.PaymentEvidenceRunID != nil || topUp.PaymentProviderTradeNo == nil || topUp.PaymentProviderTradeKey == nil {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	normalized, key, err := NormalizeEpayProviderTradeIdentity(*topUp.PaymentProviderTradeNo)
	if err != nil || normalized != *topUp.PaymentProviderTradeNo || key != *topUp.PaymentProviderTradeKey {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	return nil
}

func validateLegacyInvoicePaymentSourceEvidence(tx *gorm.DB, topUp *TopUp) error {
	if topUp.PaymentEvidenceRunID == nil || topUp.PaymentProviderTradeNo != nil || topUp.PaymentProviderTradeKey != nil {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	var run InvoicePaymentEvidenceBackfillRun
	if err := tx.First(&run, *topUp.PaymentEvidenceRunID).Error; err != nil || run.Status != InvoicePaymentEvidenceRunStatusCompleted ||
		run.CompletedAt == nil || run.PolicyVersion != constant.InvoicePaymentEvidencePolicyVersion ||
		run.CanonicalPolicyJSON != constant.InvoicePaymentEvidenceCanonicalPolicyJSON || run.PolicySHA256 != constant.InvoicePaymentEvidencePolicySHA256 {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	fingerprint, amount, err := invoicePaymentEvidenceFingerprint(topUp)
	if err != nil || amount != *topUp.PaidAmountMinor {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	var item InvoicePaymentEvidenceBackfillItem
	if err := tx.Where("run_id = ? AND topup_id = ?", run.ID, topUp.Id).First(&item).Error; err != nil ||
		item.ExpectedAmountMinor != amount || item.SourceFingerprint != fingerprint {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	return nil
}

// ClaimTopUpsTx validates and atomically binds versioned top-up evidence to an invoice application.
func (s *TopUpInvoicePaymentSource) ClaimTopUpsTx(tx *gorm.DB, request ClaimInvoiceTopUpsRequest) ([]ClaimedTopUpEvidence, error) {
	ordered, err := validateInvoicePaymentSourceInput(tx, request.UserID, request.ApplicationID, request.TopUps)
	if err != nil {
		return nil, err
	}
	rows, err := loadClaimableInvoiceTopUps(tx, request.UserID, ordered)
	if err != nil {
		return nil, err
	}
	return claimInvoiceTopUps(tx, request, rows, ordered)
}

func loadClaimableInvoiceTopUps(tx *gorm.DB, userID int, ordered []TopUpVersionExpectation) ([]TopUp, error) {
	rows := make([]TopUp, 0, len(ordered))
	for _, expected := range ordered {
		var topUp TopUp
		if err := lockForUpdate(tx).First(&topUp, expected.TopUpID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrInvoicePaymentSourceNotEligible
			}
			return nil, err
		}
		if topUp.InvoiceApplicationID != nil || topUp.PaymentVersion == nil || *topUp.PaymentVersion != expected.ExpectedPaymentVersion {
			return nil, ErrInvoicePaymentSourceClaimConflict
		}
		if err := validateInvoicePaymentSourceCommon(tx, &topUp, userID); err != nil {
			return nil, err
		}
		if err := validateInvoicePaymentSourceEvidence(tx, &topUp); err != nil {
			return nil, err
		}
		rows = append(rows, topUp)
	}
	return rows, nil
}

func claimInvoiceTopUps(tx *gorm.DB, request ClaimInvoiceTopUpsRequest, rows []TopUp, ordered []TopUpVersionExpectation) ([]ClaimedTopUpEvidence, error) {
	claimed := make([]ClaimedTopUpEvidence, 0, len(rows))
	for i := range rows {
		topUp := &rows[i]
		expectedVersion := ordered[i].ExpectedPaymentVersion
		nextVersion := expectedVersion + 1
		result := tx.Model(&TopUp{}).
			Where("id = ? AND user_id = ? AND payment_version = ? AND invoice_application_id IS NULL", topUp.Id, request.UserID, expectedVersion).
			Updates(map[string]any{"invoice_application_id": request.ApplicationID, "payment_version": nextVersion})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, ErrInvoicePaymentSourceClaimConflict
		}
		claimed = append(claimed, ClaimedTopUpEvidence{
			TopUpID: topUp.Id, MerchantTradeNo: topUp.TradeNo, PaidAmountMinor: *topUp.PaidAmountMinor,
			Currency: *topUp.Currency, ProductSnapshot: *topUp.ProductSnapshot, PaidAt: topUp.CompleteTime,
			EvidenceSource: *topUp.PaymentEvidenceSource, PaymentVersion: nextVersion,
		})
	}
	return claimed, nil
}

// ReleaseTopUpsTx atomically removes an application's claims while advancing each payment version.
func (s *TopUpInvoicePaymentSource) ReleaseTopUpsTx(tx *gorm.DB, request ReleaseInvoiceTopUpsRequest) ([]ReleasedTopUpVersion, error) {
	ordered, err := validateInvoicePaymentSourceInput(tx, request.UserID, request.ApplicationID, request.TopUps)
	if err != nil {
		return nil, err
	}
	rows, err := loadReleasableInvoiceTopUps(tx, request, ordered)
	if err != nil {
		return nil, err
	}
	return releaseInvoiceTopUps(tx, request, rows, ordered)
}

func loadReleasableInvoiceTopUps(tx *gorm.DB, request ReleaseInvoiceTopUpsRequest, ordered []TopUpVersionExpectation) ([]TopUp, error) {
	rows := make([]TopUp, 0, len(ordered))
	for _, expected := range ordered {
		var topUp TopUp
		if err := lockForUpdate(tx).First(&topUp, expected.TopUpID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrInvoicePaymentSourceClaimConflict
			}
			return nil, err
		}
		if topUp.UserId != request.UserID || topUp.InvoiceApplicationID == nil || *topUp.InvoiceApplicationID != request.ApplicationID ||
			topUp.PaymentVersion == nil || *topUp.PaymentVersion != expected.ExpectedPaymentVersion {
			return nil, ErrInvoicePaymentSourceClaimConflict
		}
		if err := validateInvoicePaymentSourceImmutableEvidence(tx, &topUp); err != nil {
			return nil, err
		}
		rows = append(rows, topUp)
	}
	return rows, nil
}

func releaseInvoiceTopUps(tx *gorm.DB, request ReleaseInvoiceTopUpsRequest, rows []TopUp, ordered []TopUpVersionExpectation) ([]ReleasedTopUpVersion, error) {
	released := make([]ReleasedTopUpVersion, 0, len(rows))
	for i := range rows {
		topUp := &rows[i]
		expectedVersion := ordered[i].ExpectedPaymentVersion
		nextVersion := expectedVersion + 1
		result := tx.Model(&TopUp{}).
			Where("id = ? AND user_id = ? AND payment_version = ? AND invoice_application_id = ?", topUp.Id, request.UserID, expectedVersion, request.ApplicationID).
			Updates(map[string]any{"invoice_application_id": nil, "payment_version": nextVersion})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, ErrInvoicePaymentSourceClaimConflict
		}
		released = append(released, ReleasedTopUpVersion{TopUpID: topUp.Id, PaymentVersion: nextVersion})
	}
	return released, nil
}
