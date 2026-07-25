package model

import (
	"errors"
	"strings"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	invoiceIssuanceApplicationIndex = "uk_invoice_issuances_application"
	invoiceIssuanceNumberIndex      = "uk_invoice_issuances_number"
)

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

func isInvoiceIssuanceUniquenessConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var postgresErr *pgconn.PgError
	if errors.As(err, &postgresErr) {
		return postgresErr.Code == "23505" &&
			(postgresErr.ConstraintName == invoiceIssuanceApplicationIndex || postgresErr.ConstraintName == invoiceIssuanceNumberIndex)
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		message := strings.ToLower(mysqlErr.Message)
		return mysqlErr.Number == 1062 &&
			(strings.Contains(message, invoiceIssuanceApplicationIndex) || strings.Contains(message, invoiceIssuanceNumberIndex))
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed: invoice_issuances.application_id") ||
		strings.Contains(message, "unique constraint failed: invoice_issuances.invoice_number")
}
