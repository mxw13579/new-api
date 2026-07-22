package model

import "gorm.io/gorm"

// InvoiceIssuance stores the immutable fiscal identity and face value assigned to one application.
type InvoiceIssuance struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	ApplicationID   int64  `json:"application_id" gorm:"not null;uniqueIndex:uk_invoice_issuances_application"`
	InvoiceNumber   string `json:"invoice_number" gorm:"type:varchar(128);not null;uniqueIndex:uk_invoice_issuances_number"`
	InvoiceCode     string `json:"invoice_code" gorm:"type:varchar(128)"`
	InvoiceDate     int64  `json:"invoice_date" gorm:"not null"`
	FaceAmountMinor int64  `json:"face_amount_minor" gorm:"not null"`
	Currency        string `json:"currency" gorm:"type:char(3);not null"`
	CreatedBy       int    `json:"created_by" gorm:"not null"`
	CreatedAt       int64  `json:"created_at" gorm:"not null"`
	UpdatedAt       int64  `json:"updated_at" gorm:"not null"`
}

// BeforeUpdate rejects mutation of persisted issuance facts after creation.
func (InvoiceIssuance) BeforeUpdate(*gorm.DB) error {
	return ErrInvoiceIssuanceConflict
}
