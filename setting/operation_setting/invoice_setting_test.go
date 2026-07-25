package operation_setting

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
)

func TestInvoiceSettingSnapshotPublicationIsAtomic(t *testing.T) {
	previous := GetInvoiceSetting()
	t.Cleanup(func() { PublishInvoiceSetting(previous) })

	first := previous
	first.ApplicationWindowDays = 11
	first.R2Endpoint = "https://11111111111111111111111111111111.r2.cloudflarestorage.com"
	first.R2Bucket = "bucket-1"
	first.R2AccessKeyID = "access-1"
	first.R2Secret = "secret-1"
	second := first
	second.ApplicationWindowDays = 22
	second.R2Endpoint = "https://22222222222222222222222222222222.r2.cloudflarestorage.com"
	second.R2Bucket = "bucket-2"
	second.R2AccessKeyID = "access-2"
	second.R2Secret = "secret-2"

	PublishInvoiceSetting(first)
	var wait sync.WaitGroup
	start := make(chan struct{})
	errors := make(chan string, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range 5_000 {
				setting := GetInvoiceSetting()
				suffix := setting.R2Bucket[len(setting.R2Bucket)-1:]
				if setting.R2AccessKeyID != "access-"+suffix || setting.R2Secret != "secret-"+suffix ||
					setting.R2Endpoint[8:9] != suffix || setting.ApplicationWindowDays != map[string]int{"1": 11, "2": 22}[suffix] {
					select {
					case errors <- fmt.Sprintf("torn snapshot: %+v", setting):
					default:
					}
					return
				}
			}
		}()
	}
	close(start)
	for range 5_000 {
		PublishInvoiceSetting(first)
		PublishInvoiceSetting(second)
	}
	wait.Wait()
	close(errors)
	assert.Empty(t, errors)
}

func TestRegisteredConfigCannotMutatePublishedSnapshot(t *testing.T) {
	previous := GetInvoiceSetting()
	t.Cleanup(func() { PublishInvoiceSetting(previous) })
	registered := config.GlobalConfig.Get("invoice_setting").(*InvoiceSetting)
	registeredPrevious := *registered
	t.Cleanup(func() { *registered = registeredPrevious })

	registered.R2Bucket = "mutated-registry-bucket"
	assert.NotEqual(t, registered.R2Bucket, GetInvoiceSetting().R2Bucket)
}

func TestInvoiceSettingIsRegistered(t *testing.T) {
	_, ok := config.GlobalConfig.Get("invoice_setting").(*InvoiceSetting)
	assert.True(t, ok)
}

func TestInvoiceSettingDefaultsAndValidation(t *testing.T) {
	setting := DefaultInvoiceSetting()
	assert.False(t, setting.PersonalEnabled)
	assert.False(t, setting.CompanyEnabled)
	assert.Positive(t, setting.ApplicationWindowDays)
	assert.Zero(t, setting.MinimumAmountMinor)
	assert.Zero(t, setting.FeePercent)
	assert.Positive(t, setting.PDFRetentionDays)
	assert.Empty(t, setting.R2Endpoint)
	assert.Empty(t, setting.R2Bucket)
	assert.Empty(t, setting.R2AccessKeyID)
	assert.Empty(t, setting.R2Secret)
	assert.NoError(t, setting.Validate())

	invalid := []InvoiceSetting{
		{ApplicationWindowDays: 0, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, MinimumAmountMinor: -1, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, FeePercent: -1, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, FeePercent: 101, PDFRetentionDays: 1},
		{ApplicationWindowDays: 1, PDFRetentionDays: 0},
		{ApplicationWindowDays: 1, PDFRetentionDays: 1, R2Endpoint: "https://example.r2.cloudflarestorage.com"},
	}
	for _, candidate := range invalid {
		assert.Error(t, candidate.Validate())
	}
}
