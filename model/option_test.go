package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRechargeRebateRatioForInviterOptionMapDefault(t *testing.T) {
	originalOptionMap := common.OptionMap
	originalRatio := common.RechargeRebateRatioForInviter
	common.RechargeRebateRatioForInviter = 0
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
		common.RechargeRebateRatioForInviter = originalRatio
	})

	InitOptionMap()

	require.Equal(t, "0", common.OptionMap["RechargeRebateRatioForInviter"])
}

func TestInvoiceSettingUpdateSerializesCommitPublishAndSecretPreservation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	previousDB := DB
	DB = db
	previousSetting := operation_setting.GetInvoiceSetting()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		DB = previousDB
		operation_setting.PublishInvoiceSetting(previousSetting)
		common.OptionMap = previousOptionMap
	})
	operation_setting.PublishInvoiceSetting(operation_setting.InvoiceSetting{R2Secret: "secret-old"})

	aCommitted := make(chan struct{})
	releaseA := make(chan struct{})
	updateA := func() error {
		return updateInvoiceSettingOptions(func(operation_setting.InvoiceSetting) (map[string]string, error) {
			return completeInvoiceSettingOptions("a", "secret-a"), nil
		}, func() {
			close(aCommitted)
			<-releaseA
		})
	}
	updateB := func() error {
		return UpdateInvoiceSettingOptions(func(current operation_setting.InvoiceSetting) (map[string]string, error) {
			return completeInvoiceSettingOptions("b", current.R2Secret), nil
		})
	}

	var wait sync.WaitGroup
	wait.Add(1)
	var errA error
	go func() { defer wait.Done(); errA = updateA() }()
	<-aCommitted
	bDone := make(chan error, 1)
	go func() { bDone <- updateB() }()
	select {
	case err := <-bDone:
		require.Failf(t, "concurrent update escaped serial section", "B completed before A published: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseA)
	wait.Wait()
	require.NoError(t, errA)
	require.NoError(t, <-bDone)

	setting := operation_setting.GetInvoiceSetting()
	require.Equal(t, "bucket-b", setting.R2Bucket)
	require.Equal(t, "secret-a", setting.R2Secret, "blank B secret preserves A's committed rotation")
	var bucket, secret Option
	require.NoError(t, db.First(&bucket, "key = ?", "invoice_setting.r2_bucket").Error)
	require.NoError(t, db.First(&secret, "key = ?", "invoice_setting.r2_secret").Error)
	require.Equal(t, setting.R2Bucket, bucket.Value)
	require.Equal(t, setting.R2Secret, secret.Value)
}

func TestInvoiceSettingSyncCannotPublishAStaleDatabaseSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	previousDB := DB
	DB = db
	previousSetting := operation_setting.GetInvoiceSetting()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		DB = previousDB
		operation_setting.PublishInvoiceSetting(previousSetting)
		common.OptionMap = previousOptionMap
	})
	require.NoError(t, UpdateInvoiceSettingOptions(func(operation_setting.InvoiceSetting) (map[string]string, error) {
		return completeInvoiceSettingOptions("a", "secret-a"), nil
	}))

	loaded := make(chan struct{})
	releaseLoad := make(chan struct{})
	loadDone := make(chan struct{})
	go func() {
		loadOptionsFromDatabaseWithHook(func() {
			close(loaded)
			<-releaseLoad
		})
		close(loadDone)
	}()
	<-loaded
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- UpdateInvoiceSettingOptions(func(operation_setting.InvoiceSetting) (map[string]string, error) {
			return completeInvoiceSettingOptions("b", "secret-b"), nil
		})
	}()
	select {
	case err := <-updateDone:
		require.Failf(t, "update escaped sync serialization", "update completed before the loaded snapshot published: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseLoad)
	<-loadDone
	require.NoError(t, <-updateDone)

	setting := operation_setting.GetInvoiceSetting()
	require.Equal(t, "bucket-b", setting.R2Bucket)
	var bucket Option
	require.NoError(t, db.First(&bucket, "key = ?", "invoice_setting.r2_bucket").Error)
	require.Equal(t, bucket.Value, setting.R2Bucket)

	require.NoError(t, UpdateOption("invoice_setting.r2_bucket", "bucket-single"))
	require.Equal(t, "bucket-single", operation_setting.GetInvoiceSetting().R2Bucket)
	require.NoError(t, db.First(&bucket, "key = ?", "invoice_setting.r2_bucket").Error)
	require.Equal(t, "bucket-single", bucket.Value)
}

func TestInvoiceSettingSingleKeyRejectsInvalidCandidateBeforeDatabaseWrite(t *testing.T) {
	originalSetting := operation_setting.GetInvoiceSetting()
	t.Cleanup(func() { operation_setting.PublishInvoiceSetting(originalSetting) })
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "invalid numeric", key: "invoice_setting.fee_percent", value: "not-a-number"},
		{name: "unknown key", key: "invoice_setting.unknown", value: "value"},
		{name: "incomplete R2", key: "invoice_setting.r2_endpoint", value: "https://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.r2.cloudflarestorage.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&Option{}))
			previousDB := DB
			DB = db
			previousSetting := operation_setting.DefaultInvoiceSetting()
			operation_setting.PublishInvoiceSetting(previousSetting)
			previousOptionMap := common.OptionMap
			common.OptionMap = map[string]string{"sentinel": "unchanged"}
			t.Cleanup(func() {
				DB = previousDB
				common.OptionMap = previousOptionMap
			})

			require.Error(t, UpdateOption(test.key, test.value))
			var count int64
			require.NoError(t, db.Model(&Option{}).Count(&count).Error)
			require.Zero(t, count)
			require.Equal(t, map[string]string{"sentinel": "unchanged"}, common.OptionMap)
			require.Equal(t, previousSetting, operation_setting.GetInvoiceSetting())
		})
	}
}

func completeInvoiceSettingOptions(version, secret string) map[string]string {
	return map[string]string{
		"invoice_setting.personal_enabled":        "true",
		"invoice_setting.company_enabled":         "true",
		"invoice_setting.application_window_days": "30",
		"invoice_setting.minimum_amount_minor":    "0",
		"invoice_setting.fee_percent":             "1",
		"invoice_setting.pdf_retention_days":      "30",
		"invoice_setting.r2_endpoint":             "https://" + map[string]string{"a": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "b": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}[version] + ".r2.cloudflarestorage.com",
		"invoice_setting.r2_bucket":               "bucket-" + version,
		"invoice_setting.r2_access_key_id":        "access-" + version,
		"invoice_setting.r2_secret":               secret,
	}
}

func TestUpdateOptionMapParsesRechargeRebateRatioForInviter(t *testing.T) {
	originalOptionMap := common.OptionMap
	originalRatio := common.RechargeRebateRatioForInviter
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
		common.RechargeRebateRatioForInviter = originalRatio
	})

	err := updateOptionMap("RechargeRebateRatioForInviter", "5")

	require.NoError(t, err)
	require.Equal(t, "5", common.OptionMap["RechargeRebateRatioForInviter"])
	require.Equal(t, 5.0, common.RechargeRebateRatioForInviter)
}

func TestLoadOptionsMarksRetiredInvoiceFeeQuotaForExplicitMigration(t *testing.T) {
	previous := operation_setting.GetInvoiceSetting()
	t.Cleanup(func() { operation_setting.PublishInvoiceSetting(previous) })

	require.NoError(t, publishInvoiceOptions(map[string]string{
		"invoice_setting.fee_quota": "500",
	}))
	require.True(t, operation_setting.GetInvoiceSetting().FeePercentMigrationRequired)

	require.NoError(t, publishInvoiceOptions(map[string]string{
		"invoice_setting.fee_quota":   "500",
		"invoice_setting.fee_percent": "0",
	}))
	require.False(t, operation_setting.GetInvoiceSetting().FeePercentMigrationRequired)
}

func TestUpdateOptionRejectsInvalidToolPricesWithoutChangingDatabaseOrOptionMap(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	const originalValue = `{"web_search":10}`
	require.NoError(t, db.Create(&Option{Key: operation_setting.ToolPriceOptionKey, Value: originalValue}).Error)

	previousDB := DB
	previousOptionMap := common.OptionMap
	DB = db
	common.OptionMap = map[string]string{operation_setting.ToolPriceOptionKey: originalValue}
	t.Cleanup(func() {
		DB = previousDB
		common.OptionMap = previousOptionMap
	})

	require.Error(t, UpdateOption(operation_setting.ToolPriceOptionKey, `{"web_search":-1}`))

	var stored Option
	require.NoError(t, db.First(&stored, "key = ?", operation_setting.ToolPriceOptionKey).Error)
	require.Equal(t, originalValue, stored.Value)
	require.Equal(t, map[string]string{operation_setting.ToolPriceOptionKey: originalValue}, common.OptionMap)
}

func TestUpdateOptionsBulkRejectsInvalidToolPricesWithoutChangingDatabaseOrOptionMap(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	const originalToolPrices = `{"web_search":10}`
	require.NoError(t, db.Create(&Option{Key: operation_setting.ToolPriceOptionKey, Value: originalToolPrices}).Error)
	require.NoError(t, db.Create(&Option{Key: "SystemName", Value: "before"}).Error)

	previousDB := DB
	previousOptionMap := common.OptionMap
	DB = db
	common.OptionMap = map[string]string{
		operation_setting.ToolPriceOptionKey: originalToolPrices,
		"SystemName":                         "before",
	}
	t.Cleanup(func() {
		DB = previousDB
		common.OptionMap = previousOptionMap
	})

	require.Error(t, UpdateOptionsBulk(map[string]string{
		operation_setting.ToolPriceOptionKey: `{"web_search":"invalid"}`,
		"SystemName":                         "after",
	}))

	var storedToolPrices Option
	var storedSystemName Option
	require.NoError(t, db.First(&storedToolPrices, "key = ?", operation_setting.ToolPriceOptionKey).Error)
	require.NoError(t, db.First(&storedSystemName, "key = ?", "SystemName").Error)
	require.Equal(t, originalToolPrices, storedToolPrices.Value)
	require.Equal(t, "before", storedSystemName.Value)
	require.Equal(t, map[string]string{
		operation_setting.ToolPriceOptionKey: originalToolPrices,
		"SystemName":                         "before",
	}, common.OptionMap)
}
