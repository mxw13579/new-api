package model

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
)

var (
	// ErrInvoiceInvalidProfile indicates invoice profile facts that violate personal or company requirements.
	ErrInvoiceInvalidProfile = errors.New("invalid invoice profile")
	// ErrInvoiceProfileVersionConflict indicates that a profile changed after the caller read its version.
	ErrInvoiceProfileVersionConflict = errors.New("invoice profile version conflict")
	// ErrInvoiceIdempotencyConflict indicates reuse of a request identifier with different invoice facts.
	ErrInvoiceIdempotencyConflict = errors.New("invoice idempotency conflict")
	// ErrInvoiceQuotaInsufficient indicates that the user's balance cannot cover the configured invoice fee.
	ErrInvoiceQuotaInsufficient = errors.New("invoice quota insufficient")
	// ErrInvoiceTopUpIneligible indicates that one or more selected top-ups cannot be invoiced.
	ErrInvoiceTopUpIneligible = errors.New("invoice topup ineligible")
)

// InvoiceProfile stores versioned buyer identity facts and per-type default selection for one user.
type InvoiceProfile struct {
	ID                 int64  `json:"id"`
	UserID             int    `json:"user_id" gorm:"not null;index:idx_invoice_profiles_user_type,priority:1"`
	Type               string `json:"type" gorm:"type:varchar(16);not null;index:idx_invoice_profiles_user_type,priority:2"`
	Title              string `json:"title" gorm:"type:varchar(200);not null"`
	TaxNumber          string `json:"tax_number" gorm:"type:varchar(64);not null"`
	IdentityCardNumber string `json:"identity_card_number" gorm:"type:varchar(64);not null;default:''"`
	IsDefault          bool   `json:"is_default" gorm:"not null"`
	Version            int64  `json:"version" gorm:"not null"`
	CreatedAt          int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt          int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

func normalizeInvoiceProfile(profileType, title, taxNumber, identityCardNumber string) (string, string, string, error) {
	title = strings.TrimSpace(title)
	taxNumber = strings.TrimSpace(taxNumber)
	identityCardNumber = strings.TrimSpace(identityCardNumber)
	if title == "" || len([]rune(title)) > 200 || len(taxNumber) > 64 || len(identityCardNumber) > 64 {
		return "", "", "", ErrInvoiceInvalidProfile
	}
	switch profileType {
	case constant.InvoiceTypePersonal:
		if identityCardNumber == "" {
			return "", "", "", ErrInvoiceInvalidProfile
		}
		if taxNumber != "" {
			return "", "", "", ErrInvoiceInvalidProfile
		}
	case constant.InvoiceTypeCompany:
		if taxNumber == "" {
			if identityCardNumber != "" {
				return "", "", "", ErrInvoiceInvalidProfile
			}
			return "", "", "", ErrInvoiceInvalidProfile
		}
		if identityCardNumber != "" {
			return "", "", "", ErrInvoiceInvalidProfile
		}
		identityCardNumber = ""
	default:
		return "", "", "", ErrInvoiceInvalidProfile
	}
	return title, taxNumber, identityCardNumber, nil
}

func isRetryableInvoiceTransactionError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "deadlock") ||
		strings.Contains(message, "serialization failure") ||
		strings.Contains(message, "sqlstate 40001") ||
		strings.Contains(message, "sqlstate 40p01")
}

func runInvoiceTransaction(fn func(*gorm.DB) error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = DB.Transaction(fn)
		if !isRetryableInvoiceTransactionError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}

func lockInvoiceProfileOwner(tx *gorm.DB, userID int) error {
	if userID <= 0 {
		return ErrInvoiceNotFound
	}
	var owner User
	err := lockForUpdate(tx).Select("id").First(&owner, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrInvoiceNotFound
	}
	return err
}

// CreateInvoiceProfile creates normalized buyer facts and atomically converges the per-type default profile.
func CreateInvoiceProfile(userID int, request dto.CreateInvoiceProfileRequest) (*InvoiceProfile, error) {
	title, taxNumber, identityCardNumber, err := normalizeInvoiceProfile(request.Type, request.Title, request.TaxNumber, request.IdentityCardNumber)
	if err != nil {
		return nil, err
	}
	profile := &InvoiceProfile{
		UserID: userID, Type: request.Type, Title: title, TaxNumber: taxNumber, IdentityCardNumber: identityCardNumber,
		IsDefault: request.IsDefault, Version: 1,
	}
	err = runInvoiceTransaction(func(tx *gorm.DB) error {
		if err := lockInvoiceProfileOwner(tx, userID); err != nil {
			return err
		}
		if request.IsDefault {
			if err := tx.Model(&InvoiceProfile{}).
				Where("user_id = ? AND type = ? AND is_default = ?", userID, request.Type, true).
				Updates(map[string]any{"is_default": false, "version": gorm.Expr("version + 1")}).Error; err != nil {
				return err
			}
		}
		return tx.Create(profile).Error
	})
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// UpdateInvoiceProfile applies an expected-version update and preserves one default profile per invoice type.
func UpdateInvoiceProfile(userID int, request dto.UpdateInvoiceProfileRequest) (*InvoiceProfile, error) {
	if request.ID <= 0 || request.ExpectedVersion <= 0 {
		return nil, ErrInvoiceInvalidProfile
	}
	var updated InvoiceProfile
	err := runInvoiceTransaction(func(tx *gorm.DB) error {
		if err := lockInvoiceProfileOwner(tx, userID); err != nil {
			return err
		}
		var current InvoiceProfile
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", request.ID, userID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvoiceNotFound
			}
			return err
		}
		if current.Version != request.ExpectedVersion {
			return ErrInvoiceProfileVersionConflict
		}
		title, taxNumber, identityCardNumber, err := normalizeInvoiceProfile(current.Type, request.Title, request.TaxNumber, request.IdentityCardNumber)
		if err != nil {
			return err
		}
		if request.IsDefault {
			if err := tx.Model(&InvoiceProfile{}).
				Where("user_id = ? AND type = ? AND is_default = ? AND id <> ?", userID, current.Type, true, current.ID).
				Updates(map[string]any{"is_default": false, "version": gorm.Expr("version + 1")}).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&InvoiceProfile{}).
			Where("id = ? AND user_id = ? AND version = ?", current.ID, userID, request.ExpectedVersion).
			Updates(map[string]any{
				"title": title, "tax_number": taxNumber, "identity_card_number": identityCardNumber, "is_default": request.IsDefault,
				"version": request.ExpectedVersion + 1, "updated_at": time.Now().Unix(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrInvoiceProfileVersionConflict
		}
		return tx.First(&updated, current.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// DeleteInvoiceProfile deletes an owned profile only when its expected version still matches.
func DeleteInvoiceProfile(userID int, request dto.DeleteInvoiceProfileRequest) error {
	if request.ID <= 0 || request.ExpectedVersion <= 0 {
		return ErrInvoiceInvalidProfile
	}
	return runInvoiceTransaction(func(tx *gorm.DB) error {
		if err := lockInvoiceProfileOwner(tx, userID); err != nil {
			return err
		}
		result := tx.Where("id = ? AND user_id = ? AND version = ?", request.ID, userID, request.ExpectedVersion).
			Delete(&InvoiceProfile{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var count int64
			if err := tx.Model(&InvoiceProfile{}).Where("id = ? AND user_id = ?", request.ID, userID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return ErrInvoiceNotFound
			}
			return ErrInvoiceProfileVersionConflict
		}
		return nil
	})
}

// ListInvoiceProfiles returns all buyer profiles owned by a user in deterministic default-first order.
func ListInvoiceProfiles(userID int) ([]InvoiceProfile, error) {
	var profiles []InvoiceProfile
	err := DB.Where("user_id = ?", userID).Order("type, is_default desc, id").Find(&profiles).Error
	return profiles, err
}
