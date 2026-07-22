package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
)

func TestInvoiceSettingIsRegistered(t *testing.T) {
	assert.Same(t, GetInvoiceSetting(), config.GlobalConfig.Get("invoice_setting"))
}

func TestInvoiceSettingDefaultsAndValidation(t *testing.T) {
	setting := DefaultInvoiceSetting()
	assert.False(t, setting.PersonalEnabled)
	assert.False(t, setting.CompanyEnabled)
	assert.Positive(t, setting.ApplicationWindowDays)
	assert.Zero(t, setting.MinimumAmountMinor)
	assert.Zero(t, setting.FeeQuota)
	assert.Positive(t, setting.PDFRetentionDays)
	assert.NoError(t, setting.Validate())

	invalid := []InvoiceSetting{
		{ApplicationWindowDays: 0, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, MinimumAmountMinor: -1, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, FeeQuota: -1, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, FeeQuota: int64(common.MaxQuota) + 1, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, PDFRetentionDays: 0},
	}
	for _, candidate := range invalid {
		assert.Error(t, candidate.Validate())
	}
}
