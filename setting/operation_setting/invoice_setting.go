package operation_setting

import (
	"errors"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

// InvoiceSetting contains persisted invoice policy and private PDF storage configuration.
type InvoiceSetting struct {
	PersonalEnabled             bool   `json:"personal_enabled"`
	CompanyEnabled              bool   `json:"company_enabled"`
	ApplicationWindowDays       int    `json:"application_window_days"`
	MinimumAmountMinor          int64  `json:"minimum_amount_minor"`
	FeePercent                  int    `json:"fee_percent"`
	PDFRetentionDays            int    `json:"pdf_retention_days"`
	R2Endpoint                  string `json:"r2_endpoint"`
	R2Bucket                    string `json:"r2_bucket"`
	R2AccessKeyID               string `json:"r2_access_key_id"`
	R2Secret                    string `json:"r2_secret"`
	FeePercentMigrationRequired bool   `json:"-"`
}

const MinimumInvoiceAmountMinor int64 = 10_000

// DefaultInvoiceSetting returns the disabled-by-default invoice policy used before persisted options load.
func DefaultInvoiceSetting() InvoiceSetting {
	return InvoiceSetting{
		PersonalEnabled:       false,
		CompanyEnabled:        false,
		ApplicationWindowDays: 30,
		MinimumAmountMinor:    MinimumInvoiceAmountMinor,
		FeePercent:            0,
		PDFRetentionDays:      30,
	}
}

var invoiceSetting = DefaultInvoiceSetting()
var invoiceSettingSnapshot atomic.Pointer[InvoiceSetting]

func init() {
	initial := invoiceSetting
	invoiceSettingSnapshot.Store(&initial)
	config.GlobalConfig.Register("invoice_setting", &invoiceSetting)
}

// GetInvoiceSetting returns one immutable process-wide invoice-setting snapshot.
func GetInvoiceSetting() InvoiceSetting {
	return *invoiceSettingSnapshot.Load()
}

// PublishInvoiceSetting atomically replaces the complete runtime invoice setting.
// Callers must not mutate a setting after publishing it.
func PublishInvoiceSetting(setting InvoiceSetting) {
	snapshot := setting
	invoiceSettingSnapshot.Store(&snapshot)
}

// R2Configured reports whether every credential required for invoice PDF storage is present.
func (setting InvoiceSetting) R2Configured() bool {
	return strings.TrimSpace(setting.R2Endpoint) != "" && strings.TrimSpace(setting.R2Bucket) != "" &&
		strings.TrimSpace(setting.R2AccessKeyID) != "" && strings.TrimSpace(setting.R2Secret) != ""
}

// Validate rejects unsafe policy values and partially populated R2 credentials.
func (setting InvoiceSetting) Validate() error {
	if setting.ApplicationWindowDays <= 0 {
		return errors.New("invoice application window must be positive")
	}
	if setting.MinimumAmountMinor < MinimumInvoiceAmountMinor {
		return errors.New("invoice minimum amount must be at least 100 CNY")
	}
	if setting.FeePercent < 0 || setting.FeePercent > 100 {
		return errors.New("invoice fee percentage is out of range")
	}
	if setting.PDFRetentionDays <= 0 {
		return errors.New("invoice PDF retention must be positive")
	}
	r2FieldCount := 0
	for _, value := range []string{setting.R2Endpoint, setting.R2Bucket, setting.R2AccessKeyID, setting.R2Secret} {
		if strings.TrimSpace(value) != "" {
			r2FieldCount++
		}
	}
	if r2FieldCount != 0 && r2FieldCount != 4 {
		return errors.New("invoice R2 configuration must be complete")
	}
	return nil
}
