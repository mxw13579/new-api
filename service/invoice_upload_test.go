package service

import (
	"bytes"
	"context"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
