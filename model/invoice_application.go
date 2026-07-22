package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// InvoiceFeeMethodWalletQuota identifies invoice fees charged from the user's quota balance.
	InvoiceFeeMethodWalletQuota = "wallet_quota"

	// InvoiceFeeEntryTypeCharge identifies the initial invoice fee debit.
	InvoiceFeeEntryTypeCharge = "charge"
	// InvoiceFeeEntryTypeRefund identifies an invoice fee credit created during cancellation or rejection.
	InvoiceFeeEntryTypeRefund = "refund"

	// InvoiceFeeEntryStatusApplied indicates that a fee ledger entry has changed the user's balance.
	InvoiceFeeEntryStatusApplied = "applied"
	// InvoiceFeeEntryStatusPending indicates that a fee refund awaits sufficient balance headroom.
	InvoiceFeeEntryStatusPending = "pending"
)

// InvoiceApplication is the aggregate root for an invoice request, its review state, and fee settlement links.
type InvoiceApplication struct {
	ID                  int64  `json:"id"`
	ApplicationNo       string `json:"application_no" gorm:"type:varchar(64);not null;uniqueIndex"`
	UserID              int    `json:"user_id" gorm:"not null;uniqueIndex:uidx_invoice_app_user_request,priority:1;index:idx_invoice_app_user_submitted,priority:1"`
	RequestID           string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex:uidx_invoice_app_user_request,priority:2"`
	RequestFingerprint  string `json:"request_fingerprint" gorm:"type:char(64);not null"`
	Type                string `json:"type" gorm:"type:varchar(16);not null"`
	Status              string `json:"status" gorm:"type:varchar(32);not null;index:idx_invoice_app_status_submitted,priority:1"`
	PaymentReviewStatus string `json:"payment_review_status" gorm:"type:varchar(32);not null"`
	Currency            string `json:"currency" gorm:"type:varchar(3);not null"`
	AmountMinor         int64  `json:"amount_minor" gorm:"not null"`
	FeeQuota            int    `json:"fee_quota" gorm:"not null"`
	FeeMethod           string `json:"fee_method" gorm:"type:varchar(32);not null"`
	FeeStatus           string `json:"fee_status" gorm:"type:varchar(32);not null"`
	ProfileSnapshot     string `json:"profile_snapshot" gorm:"type:text;not null"`
	PolicySnapshot      string `json:"policy_snapshot" gorm:"type:text;not null"`
	ActiveDocumentID    *int64 `json:"active_document_id"`
	FeeChargeEntryID    *int64 `json:"fee_charge_entry_id"`
	FeeRefundEntryID    *int64 `json:"fee_refund_entry_id"`
	RejectReason        string `json:"reject_reason" gorm:"type:text;not null"`
	SubmittedAt         int64  `json:"submitted_at" gorm:"not null;index:idx_invoice_app_user_submitted,priority:2;index:idx_invoice_app_status_submitted,priority:2"`
	ReviewedAt          *int64 `json:"reviewed_at"`
	ReviewedBy          *int   `json:"reviewed_by"`
	CancelledAt         *int64 `json:"cancelled_at"`
	IssuedAt            *int64 `json:"issued_at"`
	CreatedAt           int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt           int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

// InvoiceItem snapshots one claimed top-up's immutable payment facts within an invoice application.
type InvoiceItem struct {
	ID                 int64  `json:"id"`
	ApplicationID      int64  `json:"application_id" gorm:"not null;uniqueIndex:uidx_invoice_item_app_topup,priority:1;index"`
	TopUpID            int    `json:"topup_id" gorm:"column:topup_id;not null;uniqueIndex:uidx_invoice_item_app_topup,priority:2;index"`
	TradeNo            string `json:"trade_no" gorm:"type:varchar(255);not null"`
	PaidAmountMinor    int64  `json:"paid_amount_minor" gorm:"not null"`
	Currency           string `json:"currency" gorm:"type:varchar(3);not null"`
	ProductDescription string `json:"product_description" gorm:"type:text;not null"`
	PaidAt             int64  `json:"paid_at" gorm:"not null"`
	CreatedAt          int64  `json:"created_at" gorm:"autoCreateTime"`
}

// InvoiceFeeLedgerEntry records an idempotent invoice fee charge or refund and its balance transition.
type InvoiceFeeLedgerEntry struct {
	ID             int64  `json:"id" gorm:"index:idx_invoice_fee_refund_settlement,priority:4"`
	ApplicationID  int64  `json:"application_id" gorm:"not null;uniqueIndex:uidx_invoice_fee_app_type,priority:1;index"`
	UserID         int    `json:"user_id" gorm:"not null;index"`
	EntryType      string `json:"entry_type" gorm:"type:varchar(16);not null;uniqueIndex:uidx_invoice_fee_app_type,priority:2;index:idx_invoice_fee_refund_settlement,priority:1"`
	Quota          int    `json:"quota" gorm:"not null"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(128);not null;uniqueIndex"`
	BalanceBefore  *int   `json:"balance_before"`
	BalanceAfter   *int   `json:"balance_after"`
	Status         string `json:"status" gorm:"type:varchar(16);not null;index;index:idx_invoice_fee_refund_settlement,priority:2"`
	LastAttemptAt  int64  `json:"-" gorm:"not null;default:0;index:idx_invoice_fee_refund_settlement,priority:3"`
	AttemptCount   int    `json:"-" gorm:"not null;default:0"`
	LastError      string `json:"-" gorm:"type:varchar(128);not null;default:''"`
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime"`
	AppliedAt      *int64 `json:"applied_at"`
}

type invoiceApplicationFingerprint struct {
	Type           string `json:"type"`
	ProfileID      int64  `json:"profile_id"`
	ProfileVersion int64  `json:"profile_version"`
	TopUpIDs       []int  `json:"topup_ids"`
}

func normalizeInvoiceTopUpIDs(topUpIDs []int) ([]int, error) {
	if len(topUpIDs) == 0 {
		return nil, ErrInvoiceTopUpIneligible
	}
	ordered := append([]int(nil), topUpIDs...)
	sort.Ints(ordered)
	normalized := ordered[:0]
	for _, topUpID := range ordered {
		if topUpID <= 0 {
			return nil, ErrInvoiceTopUpIneligible
		}
		if len(normalized) == 0 || normalized[len(normalized)-1] != topUpID {
			normalized = append(normalized, topUpID)
		}
	}
	return normalized, nil
}

func invoiceRequestFingerprint(profileType string, request dto.CreateInvoiceApplicationRequest, topUpIDs []int) (string, error) {
	payload, err := common.Marshal(invoiceApplicationFingerprint{
		Type: profileType, ProfileID: request.ProfileID, ProfileVersion: request.ProfileVersion, TopUpIDs: topUpIDs,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload)), nil
}

func lookupIdempotentInvoiceApplication(userID int, requestID, fingerprint string) (*InvoiceApplication, error) {
	var existing InvoiceApplication
	err := DB.Where("user_id = ? AND request_id = ?", userID, requestID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	if existing.RequestFingerprint != fingerprint {
		return nil, ErrInvoiceIdempotencyConflict
	}
	return &existing, nil
}

// CreateInvoiceApplication atomically snapshots a profile, claims eligible top-ups, and charges the configured fee.
func CreateInvoiceApplication(userID int, request dto.CreateInvoiceApplicationRequest, paymentSource InvoicePaymentSource) (*InvoiceApplication, error) {
	request.RequestID = strings.TrimSpace(request.RequestID)
	if userID <= 0 || request.RequestID == "" || len(request.RequestID) > 128 || request.ProfileID <= 0 || request.ProfileVersion <= 0 {
		return nil, ErrInvoiceStateConflict
	}
	topUpIDs, err := normalizeInvoiceTopUpIDs(request.TopUpIDs)
	if err != nil {
		return nil, err
	}
	var existingByKey InvoiceApplication
	existingErr := DB.Where("user_id = ? AND request_id = ?", userID, request.RequestID).First(&existingByKey).Error
	if existingErr == nil {
		fingerprint, err := invoiceRequestFingerprint(existingByKey.Type, request, topUpIDs)
		if err != nil {
			return nil, err
		}
		if existingByKey.RequestFingerprint != fingerprint {
			return nil, ErrInvoiceIdempotencyConflict
		}
		return &existingByKey, nil
	}
	if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return nil, existingErr
	}

	var profile InvoiceProfile
	profileErr := DB.Where("id = ? AND user_id = ?", request.ProfileID, userID).First(&profile).Error
	if profileErr != nil && !errors.Is(profileErr, gorm.ErrRecordNotFound) {
		return nil, profileErr
	}
	if errors.Is(profileErr, gorm.ErrRecordNotFound) {
		return nil, ErrInvoiceNotFound
	}
	fingerprint, err := invoiceRequestFingerprint(profile.Type, request, topUpIDs)
	if err != nil {
		return nil, err
	}
	if existing, lookupErr := lookupIdempotentInvoiceApplication(userID, request.RequestID, fingerprint); lookupErr == nil || !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return existing, lookupErr
	}
	if profile.Version != request.ProfileVersion {
		return nil, ErrInvoiceProfileVersionConflict
	}
	if _, _, err := normalizeInvoiceProfile(profile.Type, profile.Title, profile.TaxNumber); err != nil {
		return nil, err
	}

	setting := *operation_setting.GetInvoiceSetting()
	if err := setting.Validate(); err != nil {
		return nil, err
	}
	if (profile.Type == constant.InvoiceTypePersonal && !setting.PersonalEnabled) ||
		(profile.Type == constant.InvoiceTypeCompany && !setting.CompanyEnabled) {
		return nil, ErrInvoiceStateConflict
	}
	feeQuota := int(setting.FeeQuota)
	feeStatus := constant.InvoiceFeeStatusNotRequired
	if feeQuota > 0 {
		feeStatus = constant.InvoiceFeeStatusPaid
	}
	profileJSON, err := common.Marshal(dto.InvoiceProfileSnapshot{
		Type: profile.Type, Title: profile.Title, TaxNumber: profile.TaxNumber, Version: profile.Version,
	})
	if err != nil {
		return nil, err
	}
	policyJSON, err := common.Marshal(dto.InvoicePolicySnapshot{
		ApplicationWindowDays: setting.ApplicationWindowDays, MinimumAmountMinor: setting.MinimumAmountMinor,
		FeeQuota: setting.FeeQuota, PDFRetentionDays: setting.PDFRetentionDays,
	})
	if err != nil {
		return nil, err
	}
	if paymentSource == nil {
		paymentSource = NewTopUpInvoicePaymentSource()
	}
	application := &InvoiceApplication{
		ApplicationNo: "INV-" + uuid.NewString(), UserID: userID, RequestID: request.RequestID,
		RequestFingerprint: fingerprint, Type: profile.Type, Status: constant.InvoiceApplicationStatusSubmitted,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		FeeQuota: feeQuota, FeeMethod: InvoiceFeeMethodWalletQuota, FeeStatus: feeStatus,
		ProfileSnapshot: string(profileJSON), PolicySnapshot: string(policyJSON), SubmittedAt: time.Now().Unix(),
	}

	err = runInvoiceTransaction(func(tx *gorm.DB) error {
		if err := tx.Create(application).Error; err != nil {
			return err
		}
		var currentProfile InvoiceProfile
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", request.ProfileID, userID).First(&currentProfile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvoiceNotFound
			}
			return err
		}
		if currentProfile.Version != request.ProfileVersion || currentProfile.Type != profile.Type ||
			currentProfile.Title != profile.Title || currentProfile.TaxNumber != profile.TaxNumber {
			return ErrInvoiceProfileVersionConflict
		}

		expectations := make([]TopUpVersionExpectation, 0, len(topUpIDs))
		for _, topUpID := range topUpIDs {
			var topUp TopUp
			if err := lockForUpdate(tx).Select("id", "payment_version").First(&topUp, topUpID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrInvoiceTopUpIneligible
				}
				return err
			}
			if topUp.PaymentVersion == nil {
				return ErrInvoiceTopUpIneligible
			}
			expectations = append(expectations, TopUpVersionExpectation{TopUpID: topUpID, ExpectedPaymentVersion: *topUp.PaymentVersion})
		}
		claimed, err := paymentSource.ClaimTopUpsTx(tx, ClaimInvoiceTopUpsRequest{
			UserID: userID, ApplicationID: application.ID, TopUps: expectations,
		})
		if err != nil {
			return err
		}
		cutoff := time.Now().Unix() - int64(setting.ApplicationWindowDays)*24*60*60
		items := make([]InvoiceItem, 0, len(claimed))
		var amountMinor int64
		for _, evidence := range claimed {
			if evidence.Currency != constant.InvoiceCurrencyCNY || evidence.PaidAmountMinor <= 0 || evidence.PaidAt < cutoff ||
				amountMinor > math.MaxInt64-evidence.PaidAmountMinor {
				return ErrInvoiceTopUpIneligible
			}
			amountMinor += evidence.PaidAmountMinor
			items = append(items, InvoiceItem{
				ApplicationID: application.ID, TopUpID: evidence.TopUpID, TradeNo: evidence.MerchantTradeNo,
				PaidAmountMinor: evidence.PaidAmountMinor, Currency: evidence.Currency,
				ProductDescription: evidence.ProductSnapshot, PaidAt: evidence.PaidAt,
			})
		}
		if amountMinor < setting.MinimumAmountMinor {
			return ErrInvoiceTopUpIneligible
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		application.AmountMinor = amountMinor

		if feeQuota > 0 {
			var user User
			if err := lockForUpdate(tx).Select("id", "quota").First(&user, userID).Error; err != nil {
				return err
			}
			result := tx.Model(&User{}).Where("id = ? AND quota >= ?", userID, feeQuota).
				Update("quota", gorm.Expr("quota - ?", feeQuota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrInvoiceQuotaInsufficient
			}
			now := time.Now().Unix()
			before, after := user.Quota, user.Quota-feeQuota
			charge := InvoiceFeeLedgerEntry{
				ApplicationID: application.ID, UserID: userID, EntryType: InvoiceFeeEntryTypeCharge,
				Quota: feeQuota, IdempotencyKey: "invoice_fee:charge:" + application.ApplicationNo,
				BalanceBefore: &before, BalanceAfter: &after, Status: InvoiceFeeEntryStatusApplied, AppliedAt: &now,
			}
			if err := tx.Create(&charge).Error; err != nil {
				return err
			}
			application.FeeChargeEntryID = &charge.ID
		}
		return tx.Save(application).Error
	})
	if err != nil {
		if existing, lookupErr := lookupIdempotentInvoiceApplication(userID, request.RequestID, fingerprint); lookupErr == nil {
			return existing, nil
		} else if errors.Is(lookupErr, ErrInvoiceIdempotencyConflict) {
			return nil, lookupErr
		}
		return nil, err
	}
	if feeQuota > 0 {
		if cacheErr := invalidateUserCache(userID); cacheErr != nil {
			common.SysError("failed to invalidate invoice fee quota cache: " + cacheErr.Error())
		}
	}
	return application, nil
}

func invoiceReleaseExpectations(tx *gorm.DB, application *InvoiceApplication) ([]TopUpVersionExpectation, error) {
	var items []InvoiceItem
	if err := tx.Where("application_id = ?", application.ID).Order("topup_id").Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrInvoiceStateConflict
	}
	expectations := make([]TopUpVersionExpectation, 0, len(items))
	for _, item := range items {
		var topUp TopUp
		if err := lockForUpdate(tx).Select("id", "payment_version").First(&topUp, item.TopUpID).Error; err != nil {
			return nil, err
		}
		if topUp.PaymentVersion == nil {
			return nil, ErrInvoicePaymentSourceEvidenceConflict
		}
		expectations = append(expectations, TopUpVersionExpectation{TopUpID: item.TopUpID, ExpectedPaymentVersion: *topUp.PaymentVersion})
	}
	return expectations, nil
}

func validAppliedInvoiceFeeEntry(entry *InvoiceFeeLedgerEntry, application *InvoiceApplication, entryType string) bool {
	if entry.ApplicationID != application.ID || entry.UserID != application.UserID || entry.EntryType != entryType ||
		entry.Quota != application.FeeQuota || entry.Status != InvoiceFeeEntryStatusApplied ||
		entry.IdempotencyKey != "invoice_fee:"+entryType+":"+application.ApplicationNo ||
		entry.BalanceBefore == nil || entry.BalanceAfter == nil || entry.AppliedAt == nil || *entry.AppliedAt <= 0 {
		return false
	}
	before, after := *entry.BalanceBefore, *entry.BalanceAfter
	if before < 0 || before > common.MaxQuota || after < 0 || after > common.MaxQuota {
		return false
	}
	if entryType == InvoiceFeeEntryTypeCharge {
		return before >= after && before-after == entry.Quota
	}
	return after >= before && after-before == entry.Quota
}

func validPendingInvoiceFeeRefund(entry *InvoiceFeeLedgerEntry, application *InvoiceApplication) bool {
	return entry.ApplicationID == application.ID && entry.UserID == application.UserID &&
		entry.EntryType == InvoiceFeeEntryTypeRefund && entry.Quota == application.FeeQuota &&
		entry.IdempotencyKey == "invoice_fee:refund:"+application.ApplicationNo &&
		entry.Status == InvoiceFeeEntryStatusPending && entry.BalanceBefore == nil &&
		entry.BalanceAfter == nil && entry.AppliedAt == nil
}

func loadInvoiceFeeEntryByID(tx *gorm.DB, entryID int64) (*InvoiceFeeLedgerEntry, error) {
	var entry InvoiceFeeLedgerEntry
	if err := lockForUpdate(tx).First(&entry, entryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoiceStateConflict
		}
		return nil, err
	}
	return &entry, nil
}

func refundInvoiceFeeTx(tx *gorm.DB, application *InvoiceApplication) (bool, error) {
	if application.FeeQuota == 0 {
		if application.FeeChargeEntryID != nil || application.FeeRefundEntryID != nil ||
			application.FeeStatus != constant.InvoiceFeeStatusNotRequired {
			return false, ErrInvoiceStateConflict
		}
		application.FeeStatus = constant.InvoiceFeeStatusNotRequired
		return false, nil
	}
	if application.FeeQuota < 0 || application.FeeQuota > common.MaxQuota || application.FeeChargeEntryID == nil {
		return false, ErrInvoiceStateConflict
	}
	if application.FeeStatus != constant.InvoiceFeeStatusPaid &&
		application.FeeStatus != constant.InvoiceFeeStatusRefundPending &&
		application.FeeStatus != constant.InvoiceFeeStatusRefunded {
		return false, ErrInvoiceStateConflict
	}
	charge, err := loadInvoiceFeeEntryByID(tx, *application.FeeChargeEntryID)
	if err != nil {
		return false, err
	}
	if !validAppliedInvoiceFeeEntry(charge, application, InvoiceFeeEntryTypeCharge) {
		return false, ErrInvoiceStateConflict
	}

	var refund *InvoiceFeeLedgerEntry
	if application.FeeStatus == constant.InvoiceFeeStatusPaid {
		if application.FeeRefundEntryID != nil {
			return false, ErrInvoiceStateConflict
		}
		var existing InvoiceFeeLedgerEntry
		err := lockForUpdate(tx).Where("application_id = ? AND entry_type = ?", application.ID, InvoiceFeeEntryTypeRefund).First(&existing).Error
		if err == nil {
			return false, ErrInvoiceStateConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
		created := InvoiceFeeLedgerEntry{
			ApplicationID: application.ID, UserID: application.UserID, EntryType: InvoiceFeeEntryTypeRefund,
			Quota: application.FeeQuota, IdempotencyKey: "invoice_fee:refund:" + application.ApplicationNo,
			Status: InvoiceFeeEntryStatusPending,
		}
		if err := tx.Create(&created).Error; err != nil {
			return false, err
		}
		application.FeeRefundEntryID = &created.ID
		refund = &created
	} else {
		if application.FeeRefundEntryID == nil {
			return false, ErrInvoiceStateConflict
		}
		refund, err = loadInvoiceFeeEntryByID(tx, *application.FeeRefundEntryID)
		if err != nil {
			return false, err
		}
	}
	if application.FeeStatus == constant.InvoiceFeeStatusRefunded {
		if !validAppliedInvoiceFeeEntry(refund, application, InvoiceFeeEntryTypeRefund) {
			return false, ErrInvoiceStateConflict
		}
		return false, nil
	}
	if !validPendingInvoiceFeeRefund(refund, application) {
		return false, ErrInvoiceStateConflict
	}
	var user User
	if err := lockForUpdate(tx).Select("id", "quota").First(&user, application.UserID).Error; err != nil {
		return false, err
	}
	headroomLimit := common.MaxQuota - application.FeeQuota
	if user.Quota > headroomLimit {
		application.FeeStatus = constant.InvoiceFeeStatusRefundPending
		return false, nil
	}
	result := tx.Model(&User{}).Where("id = ? AND quota <= ?", application.UserID, headroomLimit).
		Update("quota", gorm.Expr("quota + ?", application.FeeQuota))
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		application.FeeStatus = constant.InvoiceFeeStatusRefundPending
		return false, nil
	}
	now := time.Now().Unix()
	before, after := user.Quota, user.Quota+application.FeeQuota
	result = tx.Model(&InvoiceFeeLedgerEntry{}).
		Where("id = ? AND status = ?", refund.ID, InvoiceFeeEntryStatusPending).
		Updates(map[string]any{
			"status": InvoiceFeeEntryStatusApplied, "balance_before": before,
			"balance_after": after, "applied_at": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, ErrInvoiceStateConflict
	}
	application.FeeStatus = constant.InvoiceFeeStatusRefunded
	return true, nil
}

func settleInvoiceApplication(applicationID int64, ownerID *int, actorID int, expectedStatus, targetStatus, reason string) (*InvoiceApplication, error) {
	var settled InvoiceApplication
	quotaChanged := false
	err := runInvoiceTransaction(func(tx *gorm.DB) error {
		query := lockForUpdate(tx).Where("id = ?", applicationID)
		if ownerID != nil {
			query = query.Where("user_id = ?", *ownerID)
		}
		if err := query.First(&settled).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvoiceNotFound
			}
			return err
		}
		if settled.Status == targetStatus {
			return nil
		}
		if settled.Status == constant.InvoiceApplicationStatusIssued || settled.Status != expectedStatus {
			return ErrInvoiceStateConflict
		}
		if targetStatus == constant.InvoiceApplicationStatusCancelled && settled.Status != constant.InvoiceApplicationStatusSubmitted {
			return ErrInvoiceStateConflict
		}
		if targetStatus == constant.InvoiceApplicationStatusRejected {
			if settled.Status != constant.InvoiceApplicationStatusSubmitted && settled.Status != constant.InvoiceApplicationStatusReviewing {
				return ErrInvoiceStateConflict
			}
			reason = strings.TrimSpace(reason)
			if reason == "" {
				return ErrInvoiceStateConflict
			}
		}
		expectations, err := invoiceReleaseExpectations(tx, &settled)
		if err != nil {
			return err
		}
		if _, err := NewTopUpInvoicePaymentSource().ReleaseTopUpsTx(tx, ReleaseInvoiceTopUpsRequest{
			UserID: settled.UserID, ApplicationID: settled.ID, TopUps: expectations,
		}); err != nil {
			return err
		}
		quotaChanged, err = refundInvoiceFeeTx(tx, &settled)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		settled.Status = targetStatus
		if targetStatus == constant.InvoiceApplicationStatusCancelled {
			settled.CancelledAt = &now
		} else {
			settled.RejectReason = reason
			settled.ReviewedAt = &now
			settled.ReviewedBy = &actorID
		}
		return tx.Save(&settled).Error
	})
	if err != nil {
		return nil, err
	}
	if quotaChanged {
		if cacheErr := invalidateUserCache(settled.UserID); cacheErr != nil {
			common.SysError("failed to invalidate invoice refund quota cache: " + cacheErr.Error())
		}
	}
	return &settled, nil
}

// CancelInvoiceApplication cancels an owned submitted application, releases its top-ups, and settles its fee refund.
func CancelInvoiceApplication(userID int, applicationID int64) (*InvoiceApplication, error) {
	return settleInvoiceApplication(applicationID, &userID, userID, constant.InvoiceApplicationStatusSubmitted, constant.InvoiceApplicationStatusCancelled, "")
}

// RejectInvoiceApplication rejects an application from its expected review state and releases its financial reservations.
func RejectInvoiceApplication(actorID int, applicationID int64, expectedStatus, reason string) (*InvoiceApplication, error) {
	if expectedStatus != constant.InvoiceApplicationStatusSubmitted && expectedStatus != constant.InvoiceApplicationStatusReviewing {
		return nil, ErrInvoiceStateConflict
	}
	return settleInvoiceApplication(applicationID, nil, actorID, expectedStatus, constant.InvoiceApplicationStatusRejected, reason)
}

// ApplyPendingInvoiceFeeRefund retries one headroom-blocked fee refund without crediting the user more than once.
func ApplyPendingInvoiceFeeRefund(applicationID int64) (bool, error) {
	applied := false
	userID := 0
	err := runInvoiceTransaction(func(tx *gorm.DB) error {
		var application InvoiceApplication
		if err := lockForUpdate(tx).First(&application, applicationID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvoiceNotFound
			}
			return err
		}
		userID = application.UserID
		if application.Status != constant.InvoiceApplicationStatusCancelled && application.Status != constant.InvoiceApplicationStatusRejected {
			return ErrInvoiceStateConflict
		}
		if application.FeeStatus != constant.InvoiceFeeStatusRefundPending && application.FeeStatus != constant.InvoiceFeeStatusRefunded {
			return ErrInvoiceStateConflict
		}
		changed, err := refundInvoiceFeeTx(tx, &application)
		if err != nil {
			return err
		}
		applied = changed
		return tx.Save(&application).Error
	})
	if err != nil {
		return false, err
	}
	if applied {
		if cacheErr := invalidateUserCache(userID); cacheErr != nil {
			common.SysError("failed to invalidate pending invoice refund cache: " + cacheErr.Error())
		}
	}
	return applied, nil
}

// TransitionInvoiceApplicationReview advances an application through the submitted, reviewing, and approved states.
func TransitionInvoiceApplicationReview(actorID int, applicationID int64, expectedStatus, targetStatus string) (*InvoiceApplication, error) {
	allowed := expectedStatus == constant.InvoiceApplicationStatusSubmitted && targetStatus == constant.InvoiceApplicationStatusReviewing ||
		expectedStatus == constant.InvoiceApplicationStatusReviewing && targetStatus == constant.InvoiceApplicationStatusApproved
	if !allowed {
		return nil, ErrInvoiceStateConflict
	}
	var application InvoiceApplication
	err := runInvoiceTransaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&application, applicationID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvoiceNotFound
			}
			return err
		}
		paymentReviewPermitted := application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusNone ||
			application.PaymentReviewStatus == constant.InvoicePaymentReviewStatusResolvedValid
		if application.Status != expectedStatus || application.FeeStatus != constant.InvoiceFeeStatusPaid && application.FeeStatus != constant.InvoiceFeeStatusNotRequired ||
			!paymentReviewPermitted {
			return ErrInvoiceStateConflict
		}
		now := time.Now().Unix()
		application.Status = targetStatus
		application.ReviewedAt = &now
		application.ReviewedBy = &actorID
		return tx.Save(&application).Error
	})
	if err != nil {
		return nil, err
	}
	return &application, nil
}
