package service

import (
	"bytes"
	"context"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadInvoiceDocumentUsesOneSettingSnapshot(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	seedInvoiceDocumentApplication(t, db, 77, constant.InvoiceApplicationStatusApproved, nil)
	store := newInvoiceObjectStoreStub()
	previousFactory := newInvoiceUploadStore
	previousSetting := operation_setting.GetInvoiceSetting()
	first := operation_setting.InvoiceSetting{PDFRetentionDays: 31, R2Bucket: "private"}
	second := operation_setting.InvoiceSetting{PDFRetentionDays: 92, R2Bucket: "rotated"}
	operation_setting.PublishInvoiceSetting(first)
	newInvoiceUploadStore = func(setting operation_setting.InvoiceSetting) (InvoiceObjectStore, error) {
		assert.Equal(t, first, setting)
		operation_setting.PublishInvoiceSetting(second)
		return store, nil
	}
	t.Cleanup(func() {
		newInvoiceUploadStore = previousFactory
		operation_setting.PublishInvoiceSetting(previousSetting)
	})

	err := UploadInvoiceDocument(context.Background(), 9, 77, dto.InvoiceDocumentUploadRequest{
		ExpectedStatus: constant.InvoiceApplicationStatusApproved, InvoiceNumber: "INV-77", InvoiceDate: 100,
		FaceAmountMinor: 100, Currency: constant.InvoiceCurrencyCNY, PDFFactsAttested: true,
	}, bytes.NewReader(buildInvoiceTestPDF(t, "")))
	require.NoError(t, err)
	var document model.InvoiceDocument
	require.NoError(t, db.Where("application_id = ?", 77).First(&document).Error)
	assert.Equal(t, 31, document.RetentionDaysSnapshot)
}

func TestUploadInvoiceDocumentReachesIssuedReplacementPath(t *testing.T) {
	db := openInvoiceDocumentServiceTestDB(t)
	store := newInvoiceObjectStoreStub()
	seedInvoiceDocumentApplication(t, db, 1, constant.InvoiceApplicationStatusIssued, nil)
	issuance := model.InvoiceIssuance{
		ApplicationID: 1, InvoiceNumber: "INV-1", InvoiceDate: 100, FaceAmountMinor: 100,
		Currency: constant.InvoiceCurrencyCNY, CreatedBy: 9, CreatedAt: 100, UpdatedAt: 100,
	}
	require.NoError(t, db.Create(&issuance).Error)
	oldKey := "invoices/old.pdf"
	version := int64(1)
	expiresAt := int64(400)
	old := model.InvoiceDocument{
		ApplicationID: 1, IssuanceID: &issuance.ID, Version: &version, R2Bucket: "private", ObjectKey: &oldKey,
		ContentType: model.InvoicePDFContentType, SizeBytes: 100, SHA256: "sha", Status: model.InvoiceDocumentStatusAvailable,
		OperationToken: "old-token", OperationStartedAt: 100, UploadedBy: 9, UploadedAt: 100,
		PDFFactsAttested: true, RetentionDaysSnapshot: 30, ExpiresAt: &expiresAt, CreatedAt: 100, UpdatedAt: 100,
	}
	require.NoError(t, db.Create(&old).Error)
	require.NoError(t, db.Model(&model.InvoiceApplication{}).Where("id = ?", 1).Update("active_document_id", old.ID).Error)

	err := uploadInvoiceDocument(context.Background(), db, store, "private", 30, 10, 1, dto.InvoiceDocumentUploadRequest{
		ExpectedStatus: constant.InvoiceApplicationStatusIssued, InvoiceNumber: "INV-1", InvoiceDate: 100,
		FaceAmountMinor: 100, Currency: constant.InvoiceCurrencyCNY, PDFFactsAttested: true,
	}, bytes.NewReader(buildInvoiceTestPDF(t, "")), 300)
	require.NoError(t, err)

	var reloadedOld model.InvoiceDocument
	require.NoError(t, db.First(&reloadedOld, old.ID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusSuperseded, reloadedOld.Status)
	var application model.InvoiceApplication
	require.NoError(t, db.First(&application, 1).Error)
	require.NotNil(t, application.ActiveDocumentID)
	assert.NotEqual(t, old.ID, *application.ActiveDocumentID)
	var replacement model.InvoiceDocument
	require.NoError(t, db.First(&replacement, *application.ActiveDocumentID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, replacement.Status)
	assert.Equal(t, int64(2), *replacement.Version)
	assert.Equal(t, 10, *replacement.AttestedBy)
}
