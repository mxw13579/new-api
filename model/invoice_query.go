package model

import (
	"errors"

	"gorm.io/gorm"
)

// ListEligibleInvoiceTopUps returns unclaimed top-ups whose trusted payment evidence remains invoice eligible.
func ListEligibleInvoiceTopUps(userID int, cutoff int64) ([]TopUp, error) {
	if userID <= 0 || cutoff <= 0 {
		return nil, ErrInvoicePaymentSourceInvalidRequest
	}
	var candidates []TopUp
	if err := DB.Where("user_id = ? AND complete_time >= ? AND invoice_application_id IS NULL", userID, cutoff).
		Order("complete_time DESC, id DESC").Find(&candidates).Error; err != nil {
		return nil, err
	}
	eligible := make([]TopUp, 0, len(candidates))
	for i := range candidates {
		err := validateInvoicePaymentSourceCommon(DB, &candidates[i], userID)
		if err == nil {
			err = validateInvoicePaymentSourceEvidence(DB, &candidates[i])
		}
		if err == nil {
			eligible = append(eligible, candidates[i])
			continue
		}
		if errors.Is(err, ErrInvoicePaymentSourceNotEligible) || errors.Is(err, ErrInvoicePaymentSourceEvidenceConflict) {
			continue
		}
		return nil, err
	}
	return eligible, nil
}

// ListInvoiceApplications returns a deterministic page of applications, optionally restricted to one owner.
func ListInvoiceApplications(ownerID *int, offset, limit int) ([]InvoiceApplication, int64, error) {
	if offset < 0 || limit <= 0 {
		return nil, 0, ErrInvoiceStateConflict
	}
	query := DB.Model(&InvoiceApplication{})
	if ownerID != nil {
		query = query.Where("user_id = ?", *ownerID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var applications []InvoiceApplication
	if err := query.Order("submitted_at DESC, id DESC").Offset(offset).Limit(limit).Find(&applications).Error; err != nil {
		return nil, 0, err
	}
	return applications, total, nil
}

// GetInvoiceApplication loads one application and treats owner-scope mismatches as not found.
func GetInvoiceApplication(applicationID int64, ownerID *int) (*InvoiceApplication, error) {
	if applicationID <= 0 {
		return nil, ErrInvoiceNotFound
	}
	query := DB.Where("id = ?", applicationID)
	if ownerID != nil {
		query = query.Where("user_id = ?", *ownerID)
	}
	var application InvoiceApplication
	if err := query.First(&application).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}
	return &application, nil
}

// GetInvoiceApplicationItems returns the immutable top-up snapshots attached to an application.
func GetInvoiceApplicationItems(applicationID int64) ([]InvoiceItem, error) {
	var items []InvoiceItem
	err := DB.Where("application_id = ?", applicationID).Order("id").Find(&items).Error
	return items, err
}

// GetInvoiceIssuance returns an application's immutable issuance facts when the invoice has been issued.
func GetInvoiceIssuance(applicationID int64) (*InvoiceIssuance, error) {
	var issuance InvoiceIssuance
	err := DB.Where("application_id = ?", applicationID).First(&issuance).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &issuance, err
}

// GetInvoiceDocument returns the referenced active document, or nil when no active document exists.
func GetInvoiceDocument(documentID *int64) (*InvoiceDocument, error) {
	if documentID == nil {
		return nil, nil
	}
	var document InvoiceDocument
	err := DB.First(&document, *documentID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &document, err
}
