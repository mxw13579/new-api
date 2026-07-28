package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceAdminProjectionsKeepImmutableUserIDWhenAccountIsMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.InvoiceApplication{}, &model.InvoiceItem{},
		&model.InvoiceIssuance{}, &model.InvoiceDocument{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	application := model.InvoiceApplication{
		ApplicationNo: "INV-DELETED-OWNER", UserID: 99, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: "personal", Status: "submitted", PaymentReviewStatus: "none", Currency: "CNY",
		FeeMethod: "wallet_quota", FeeStatus: "not_required", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, db.Create(&application).Error)

	adminPage, err := ListInvoiceApplicationPage(nil, 1, 20, true)
	require.NoError(t, err)
	require.Len(t, adminPage.Items, 1)
	require.NotNil(t, adminPage.Items[0].UserID)
	assert.Equal(t, 99, *adminPage.Items[0].UserID)
	assert.Nil(t, adminPage.Items[0].Username)

	detail, err := GetInvoiceApplicationDetail(application.ID, nil, false, true)
	require.NoError(t, err)
	require.NotNil(t, detail.UserID)
	assert.Equal(t, 99, *detail.UserID)

	ownerID := 99
	ownerPage, err := ListInvoiceApplicationPage(&ownerID, 1, 20, false)
	require.NoError(t, err)
	ownerJSON, err := common.Marshal(ownerPage)
	require.NoError(t, err)
	assert.NotContains(t, string(ownerJSON), "user_id")
	assert.NotContains(t, string(ownerJSON), "username")
}

func TestInvoiceGlobalProjectionOmitsIdentityUnlessExplicitlyRequested(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.InvoiceApplication{}, &model.InvoiceItem{},
		&model.InvoiceIssuance{}, &model.InvoiceDocument{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	user := model.User{Username: "upload-owner", DisplayName: "Upload Owner", AffCode: "upload-owner-aff"}
	require.NoError(t, db.Create(&user).Error)
	application := model.InvoiceApplication{
		ApplicationNo: "INV-UPLOAD-ONLY", UserID: user.Id, RequestID: "request", RequestFingerprint: "fingerprint",
		Type: "personal", Status: "approved", PaymentReviewStatus: "none", Currency: "CNY",
		FeeMethod: "wallet_quota", FeeStatus: "not_required", ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, db.Create(&application).Error)

	detail, err := GetInvoiceApplicationDetail(application.ID, nil, false, false)
	require.NoError(t, err)
	payload, err := common.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(payload), "user_id")
	assert.NotContains(t, string(payload), "upload-owner")
}
