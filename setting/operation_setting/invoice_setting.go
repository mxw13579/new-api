package operation_setting

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// InvoiceSetting contains only the business policy snapshotted by the invoice
// domain. Registration with the global option catalog is integration-owned.
type InvoiceSetting struct {
	PersonalEnabled       bool  `json:"personal_enabled"`
	CompanyEnabled        bool  `json:"company_enabled"`
	ApplicationWindowDays int   `json:"application_window_days"`
	MinimumAmountMinor    int64 `json:"minimum_amount_minor"`
	FeeQuota              int64 `json:"fee_quota"`
	PDFRetentionDays      int   `json:"pdf_retention_days"`
}

func DefaultInvoiceSetting() InvoiceSetting {
	return InvoiceSetting{
		PersonalEnabled:       false,
		CompanyEnabled:        false,
		ApplicationWindowDays: 30,
		MinimumAmountMinor:    0,
		FeeQuota:              0,
		PDFRetentionDays:      30,
	}
}

var invoiceSetting = DefaultInvoiceSetting()

func GetInvoiceSetting() *InvoiceSetting {
	return &invoiceSetting
}

func (setting InvoiceSetting) Validate() error {
	if setting.ApplicationWindowDays <= 0 {
		return errors.New("invoice application window must be positive")
	}
	if setting.MinimumAmountMinor < 0 {
		return errors.New("invoice minimum amount cannot be negative")
	}
	if setting.FeeQuota < 0 || setting.FeeQuota > int64(common.MaxQuota) {
		return errors.New("invoice fee quota is out of range")
	}
	if setting.PDFRetentionDays <= 0 {
		return errors.New("invoice PDF retention must be positive")
	}
	return nil
}
