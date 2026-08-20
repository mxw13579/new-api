package service

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	invoiceIntegrationAuthorityID = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	invoiceIntegrationBucket      = "invoice-integration-private"
)

type invoiceIntegrationCheckpointIDs struct {
	happyApplicationID int64
	happyDocumentID    int64
	pendingApplication int64
	recoveryDocumentID int64
	deletingDocumentID int64
}

func TestInvoiceIntegrationHardeningSQLiteRestore(t *testing.T) {
	harness := openInvoiceIntegrationSQLiteHarness(t)
	bindInvoiceIntegrationDatabase(t, harness.db, common.DatabaseTypeSQLite)
	runInvoiceIntegrationHardeningScenario(t, harness)
}

func TestInvoiceIntegrationHardeningPostgreSQLRestore(t *testing.T) {
	harness, enabled := openInvoiceIntegrationPostgreSQLHarness(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN to run the isolated invoice pg_dump/pg_restore contract")
	}
	bindInvoiceIntegrationDatabase(t, harness.db, common.DatabaseTypePostgreSQL)
	runInvoiceIntegrationHardeningScenario(t, harness)
}

func runInvoiceIntegrationHardeningScenario(t *testing.T, harness invoiceRestoreHarness) {
	t.Helper()
	previousSetting := operation_setting.GetInvoiceSetting()
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 8
	operation_setting.PublishInvoiceSetting(operation_setting.InvoiceSetting{
		PersonalEnabled: true, CompanyEnabled: true, ApplicationWindowDays: 30,
		MinimumAmountMinor: operation_setting.MinimumInvoiceAmountMinor, FeePercent: 10, PDFRetentionDays: 30,
	})
	t.Cleanup(func() {
		operation_setting.PublishInvoiceSetting(previousSetting)
		common.QuotaPerUnit = previousQuotaPerUnit
	})

	store := newInvoiceIntegrationStore()
	ids := seedInvoiceIntegrationScenario(t, harness.db, store)
	restored := harness.restore(t)
	waitInvoiceIntegrationDatabase(t, restored)
	model.DB = restored

	convergeInvoiceIntegrationCheckpoints(t, restored, store, ids)
	assertInvoiceIntegrationPermanentRecords(t, restored, ids)
}

func seedInvoiceIntegrationScenario(t *testing.T, db *gorm.DB, store *invoiceIntegrationStore) invoiceIntegrationCheckpointIDs {
	t.Helper()
	happyUser := seedInvoiceIntegrationUser(t, db, "happy", 100)
	happyProfile := seedInvoiceIntegrationProfile(t, happyUser.Id, "Happy Buyer")
	seedInvoiceIntegrationTopUp(t, db, 71001, happyUser.Id, "happy", 12500)
	happyApplication, err := CreateInvoiceApplication(happyUser.Id, dto.CreateInvoiceApplicationRequest{
		RequestID: "ihc-happy", ProfileID: happyProfile.ID,
		ProfileVersion: happyProfile.Version, TopUpIDs: []int{71001},
	})
	require.NoError(t, err)
	_, err = ReviewInvoiceApplication(9001, happyApplication.ID, dto.ReviewInvoiceApplicationRequest{
		Action: "reviewing", ExpectedStatus: constant.InvoiceApplicationStatusSubmitted,
	})
	require.NoError(t, err)
	_, err = ReviewInvoiceApplication(9001, happyApplication.ID, dto.ReviewInvoiceApplicationRequest{
		Action: "approve", ExpectedStatus: constant.InvoiceApplicationStatusReviewing,
	})
	require.NoError(t, err)

	document, err := CreateInvoiceDocumentUpload(db, store.Bucket(), happyApplication.ID, 9001, 100)
	require.NoError(t, err)
	document, err = PromoteInvoiceDocument(context.Background(), db, store, document.ID, document.OperationToken,
		bytes.NewReader(buildInvoiceTestPDF(t, "")), 110)
	require.NoError(t, err)
	require.NotNil(t, document.ObjectKey)
	etag := store.etag(*document.ObjectKey)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("id = ?", document.ID).Updates(map[string]any{
		"r2_authority_id": store.AuthorityID(), "object_etag": etag,
	}).Error)
	lifecycle := NewInvoiceDocumentLifecycle(db, store, model.NewInvoiceDocumentApplicationContract(), 30)
	document, err = lifecycle.Finalize(context.Background(), FinalizeInvoiceDocumentOperation{
		ApplicationID: happyApplication.ID, DocumentID: document.ID, OperationToken: document.OperationToken,
		ExpectedStatus:              constant.InvoiceApplicationStatusApproved,
		ExpectedPaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		Issuance: model.InvoiceIssuanceFacts{
			InvoiceNumber: "IH-C-HAPPY", InvoiceDate: 120, FaceAmountMinor: 12500,
			Currency: constant.InvoiceCurrencyCNY,
		},
		PDFFactsAttested: true, AttestedBy: 9001, Now: 120,
	})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, document.Status)
	assert.Equal(t, store.AuthorityID(), *document.R2AuthorityID)
	assert.Equal(t, etag, *document.ObjectETag)

	pendingUser := seedInvoiceIntegrationUser(t, db, "pending", 100)
	pendingProfile := seedInvoiceIntegrationProfile(t, pendingUser.Id, "Pending Buyer")
	seedInvoiceIntegrationTopUp(t, db, 71002, pendingUser.Id, "pending", 10000)
	pendingApplication, err := CreateInvoiceApplication(pendingUser.Id, dto.CreateInvoiceApplicationRequest{
		RequestID: "ihc-pending", ProfileID: pendingProfile.ID,
		ProfileVersion: pendingProfile.Version, TopUpIDs: []int{71002},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", pendingUser.Id).Update("quota", common.MaxQuota).Error)
	pendingApplication, err = RejectInvoiceApplication(9001, pendingApplication.ID, dto.RejectInvoiceApplicationRequest{
		ExpectedStatus: constant.InvoiceApplicationStatusSubmitted, Reason: "durable checkpoint",
	})
	require.NoError(t, err)
	require.Equal(t, constant.InvoiceFeeStatusRefundPending, pendingApplication.FeeStatus)

	recoveryKey := "invoices/ihc-recovery.pdf"
	recoveryStagingKey := "tmp/invoices/ihc-recovery.pdf"
	store.objects[recoveryKey] = []byte("recovery final")
	store.objects[recoveryStagingKey] = []byte("recovery staging")
	recoveryETag := store.etag(recoveryKey)
	recoveryDocument := model.InvoiceDocument{
		ApplicationID: happyApplication.ID, R2AuthorityID: stringPointer(store.AuthorityID()),
		R2Bucket: store.Bucket(), StagingObjectKey: &recoveryStagingKey, ObjectKey: &recoveryKey,
		ObjectETag: &recoveryETag, ContentType: model.InvoicePDFContentType, SizeBytes: 14,
		SHA256: strings.Repeat("d", 64), Status: model.InvoiceDocumentStatusValidating,
		OperationToken: "ihc-recovery-token", OperationStartedAt: 10, UploadedBy: 9001,
		UploadedAt: 10, CreatedAt: 10, UpdatedAt: 10,
	}
	require.NoError(t, db.Create(&recoveryDocument).Error)

	deletingKey := "invoices/ihc-deleting.pdf"
	store.objects[deletingKey] = []byte("deleting")
	deletingETag := store.etag(deletingKey)
	deletingDocument := model.InvoiceDocument{
		ApplicationID: happyApplication.ID, R2AuthorityID: stringPointer(store.AuthorityID()),
		R2Bucket: store.Bucket(), ObjectKey: &deletingKey, ObjectETag: &deletingETag,
		ContentType: model.InvoicePDFContentType, SizeBytes: 8, SHA256: strings.Repeat("e", 64),
		Status: model.InvoiceDocumentStatusDeleting, OperationToken: "ihc-deleting-token",
		OperationStartedAt: 10, UploadedBy: 9001, UploadedAt: 10, CreatedAt: 10, UpdatedAt: 10,
	}
	require.NoError(t, db.Create(&deletingDocument).Error)

	return invoiceIntegrationCheckpointIDs{
		happyApplicationID: happyApplication.ID, happyDocumentID: document.ID,
		pendingApplication: pendingApplication.ID, recoveryDocumentID: recoveryDocument.ID,
		deletingDocumentID: deletingDocument.ID,
	}
}

func convergeInvoiceIntegrationCheckpoints(t *testing.T, db *gorm.DB, store *invoiceIntegrationStore, ids invoiceIntegrationCheckpointIDs) {
	t.Helper()
	var pending model.InvoiceApplication
	require.NoError(t, db.First(&pending, ids.pendingApplication).Error)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", pending.UserID).Update("quota", common.MaxQuota-pending.FeeQuota).Error)
	applied, err := ApplyPendingInvoiceFeeRefund(pending.ID)
	require.NoError(t, err)
	assert.True(t, applied)
	applied, err = ApplyPendingInvoiceFeeRefund(pending.ID)
	require.NoError(t, err)
	assert.False(t, applied)

	recovered, err := ReconcileInvoiceDocument(context.Background(), db, store, ids.recoveryDocumentID, 200, 100)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDocumentStatusUploadFailed, recovered.Status)
	assert.NotContains(t, store.objects, "invoices/ihc-recovery.pdf")
	assert.NotContains(t, store.objects, "tmp/invoices/ihc-recovery.pdf")

	cleanup, err := CleanupInvoiceDocuments(context.Background(), db, store, 200, 100, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, cleanup.Processed)
	assert.Equal(t, 1, cleanup.Deleted)
	var deleted model.InvoiceDocument
	require.NoError(t, db.First(&deleted, ids.deletingDocumentID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusDeleted, deleted.Status)
	assert.NotNil(t, deleted.DeletedAt)
}

func assertInvoiceIntegrationPermanentRecords(t *testing.T, db *gorm.DB, ids invoiceIntegrationCheckpointIDs) {
	t.Helper()
	var application model.InvoiceApplication
	require.NoError(t, db.First(&application, ids.happyApplicationID).Error)
	assert.Equal(t, constant.InvoiceApplicationStatusIssued, application.Status)
	require.NotNil(t, application.ActiveDocumentID)
	assert.Equal(t, ids.happyDocumentID, *application.ActiveDocumentID)
	var itemCount, issuanceCount, documentCount, chargeCount, refundCount int64
	require.NoError(t, db.Model(&model.InvoiceItem{}).Where("application_id = ?", ids.happyApplicationID).Count(&itemCount).Error)
	require.NoError(t, db.Model(&model.InvoiceIssuance{}).Where("application_id = ?", ids.happyApplicationID).Count(&issuanceCount).Error)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("application_id = ?", ids.happyApplicationID).Count(&documentCount).Error)
	require.NoError(t, db.Model(&model.InvoiceFeeLedgerEntry{}).Where("application_id = ? AND entry_type = ?", ids.happyApplicationID, model.InvoiceFeeEntryTypeCharge).Count(&chargeCount).Error)
	require.NoError(t, db.Model(&model.InvoiceFeeLedgerEntry{}).Where("application_id = ? AND entry_type = ?", ids.pendingApplication, model.InvoiceFeeEntryTypeRefund).Count(&refundCount).Error)
	assert.Equal(t, int64(1), itemCount)
	assert.Equal(t, int64(1), issuanceCount)
	assert.Equal(t, int64(3), documentCount)
	assert.Equal(t, int64(1), chargeCount)
	assert.Equal(t, int64(1), refundCount)
}

func seedInvoiceIntegrationUser(t *testing.T, db *gorm.DB, suffix string, quota int) model.User {
	t.Helper()
	user := model.User{Username: "ihc-" + suffix, Password: "test-password", Quota: quota, AffCode: "ihc-" + suffix}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func seedInvoiceIntegrationProfile(t *testing.T, userID int, title string) *dto.InvoiceProfile {
	t.Helper()
	profile, err := CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: title, IdentityCardNumber: "11010519491231002X", IsDefault: true,
	})
	require.NoError(t, err)
	return profile
}

func seedInvoiceIntegrationTopUp(t *testing.T, db *gorm.DB, id, userID int, suffix string, amount int64) {
	t.Helper()
	providerTradeNo := "provider-ihc-" + suffix
	_, providerKey, err := model.NormalizeEpayProviderTradeIdentity(providerTradeNo)
	require.NoError(t, err)
	now := time.Now().Unix()
	eligible := true
	refunded := int64(0)
	version := int64(1)
	currency := constant.InvoicePaymentEvidenceCurrencyCNY
	state := constant.InvoicePaymentStateSucceeded
	product := constant.InvoicePaymentEvidenceTopUpProduct
	source := constant.InvoicePaymentEvidenceSourceTrustedCallback
	topUp := model.TopUp{
		Id: id, UserId: userID, Amount: 10, Money: float64(amount) / 100,
		TradeNo: "ihc-" + suffix, PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		CompleteTime: now, Status: common.TopUpStatusSuccess, PaidAmountMinor: &amount,
		Currency: &currency, InvoiceEligible: &eligible, PaymentState: &state,
		RefundedAmountMinor: &refunded, PaymentVersion: &version, ProductSnapshot: &product,
		PaymentEvidenceSource: &source, PaymentProviderTradeNo: &providerTradeNo,
		PaymentProviderTradeKey: &providerKey,
	}
	require.NoError(t, db.Create(&topUp).Error)
}
