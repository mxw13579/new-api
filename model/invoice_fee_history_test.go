package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListInvoiceFeeHistoryIsOwnerScopedPaginatedAndNewestFirst(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&InvoiceApplication{}, &InvoiceFeeLedgerEntry{}))
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	apps := []InvoiceApplication{
		{ID: 1, ApplicationNo: "INV-OWNER-1", UserID: 10, RequestID: "r1", RequestFingerprint: "f1", Type: "personal", Status: "submitted", PaymentReviewStatus: "none", Currency: "CNY", FeeMethod: "wallet_quota", FeeStatus: "paid", ProfileSnapshot: "{}", PolicySnapshot: `{"fee_percent":5}`, SubmittedAt: 1},
		{ID: 2, ApplicationNo: "INV-OWNER-2", UserID: 10, RequestID: "r2", RequestFingerprint: "f2", Type: "personal", Status: "submitted", PaymentReviewStatus: "none", Currency: "CNY", FeeMethod: "wallet_quota", FeeStatus: "paid", ProfileSnapshot: "{}", PolicySnapshot: `{"fee_percent":8}`, SubmittedAt: 2},
		{ID: 3, ApplicationNo: "INV-OTHER", UserID: 20, RequestID: "r3", RequestFingerprint: "f3", Type: "personal", Status: "submitted", PaymentReviewStatus: "none", Currency: "CNY", FeeMethod: "wallet_quota", FeeStatus: "paid", ProfileSnapshot: "{}", PolicySnapshot: `{"fee_percent":99}`, SubmittedAt: 3},
	}
	require.NoError(t, db.Create(&apps).Error)
	before, after, applied := 100, 90, int64(200)
	entries := []InvoiceFeeLedgerEntry{
		{ID: 1, ApplicationID: 1, UserID: 10, EntryType: InvoiceFeeEntryTypeCharge, Quota: 10, IdempotencyKey: "k1", BalanceBefore: &before, BalanceAfter: &after, Status: InvoiceFeeEntryStatusApplied, CreatedAt: 100, AppliedAt: &applied},
		{ID: 2, ApplicationID: 1, UserID: 10, EntryType: InvoiceFeeEntryTypeRefund, Quota: 10, IdempotencyKey: "k2", Status: InvoiceFeeEntryStatusPending, CreatedAt: 300},
		{ID: 3, ApplicationID: 2, UserID: 10, EntryType: InvoiceFeeEntryTypeCharge, Quota: 20, IdempotencyKey: "k3", BalanceBefore: &before, BalanceAfter: &after, Status: InvoiceFeeEntryStatusApplied, CreatedAt: 200, AppliedAt: &applied},
		{ID: 4, ApplicationID: 3, UserID: 20, EntryType: InvoiceFeeEntryTypeCharge, Quota: 30, IdempotencyKey: "k4", BalanceBefore: &before, BalanceAfter: &after, Status: InvoiceFeeEntryStatusApplied, CreatedAt: 400, AppliedAt: &applied},
	}
	require.NoError(t, db.Create(&entries).Error)

	ownerID := 10
	first, total, err := ListInvoiceFeeHistory(&ownerID, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, first, 2)
	assert.Equal(t, []int64{2, 3}, []int64{first[0].ID, first[1].ID})
	assert.Equal(t, "INV-OWNER-1", first[0].ApplicationNo)
	assert.Equal(t, 5, first[0].FeePercent)
	assert.Nil(t, first[0].BalanceBefore)
	assert.Nil(t, first[0].BalanceAfter)
	assert.Nil(t, first[0].AppliedAt)

	second, _, err := ListInvoiceFeeHistory(&ownerID, 2, 2)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, int64(1), second[0].ID)
	assert.Equal(t, 5, second[0].FeePercent)
}

func TestListInvoiceFeeHistoryGlobalScopeIncludesOwnerIdentityWithoutDuplicatingLedgerRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &InvoiceApplication{}, &InvoiceFeeLedgerEntry{}))
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
	require.NoError(t, db.Create(&[]User{{Id: 10, Username: "alice", DisplayName: "Alice A", AffCode: "aff-a"}, {Id: 20, Username: "bob", AffCode: "aff-b"}}).Error)
	require.NoError(t, db.Create(&[]InvoiceApplication{
		{ID: 1, ApplicationNo: "INV-A", UserID: 10, RequestID: "r1", RequestFingerprint: "f1", Type: "personal", Status: "submitted", PaymentReviewStatus: "none", Currency: "CNY", FeeMethod: "wallet_quota", FeeStatus: "refunded", ProfileSnapshot: "{}", PolicySnapshot: `{"fee_percent":5}`, SubmittedAt: 1},
		{ID: 2, ApplicationNo: "INV-B", UserID: 20, RequestID: "r2", RequestFingerprint: "f2", Type: "personal", Status: "submitted", PaymentReviewStatus: "none", Currency: "CNY", FeeMethod: "wallet_quota", FeeStatus: "paid", ProfileSnapshot: "{}", PolicySnapshot: `{"fee_percent":7}`, SubmittedAt: 2},
	}).Error)
	require.NoError(t, db.Create(&[]InvoiceFeeLedgerEntry{
		{ID: 1, ApplicationID: 1, UserID: 10, EntryType: InvoiceFeeEntryTypeCharge, Quota: 5, IdempotencyKey: "charge-a", Status: InvoiceFeeEntryStatusApplied, CreatedAt: 100},
		{ID: 2, ApplicationID: 1, UserID: 10, EntryType: InvoiceFeeEntryTypeRefund, Quota: 5, IdempotencyKey: "refund-a", Status: InvoiceFeeEntryStatusPending, CreatedAt: 300},
		{ID: 3, ApplicationID: 2, UserID: 20, EntryType: InvoiceFeeEntryTypeCharge, Quota: 7, IdempotencyKey: "charge-b", Status: InvoiceFeeEntryStatusApplied, CreatedAt: 200},
	}).Error)
	require.NoError(t, db.Unscoped().Delete(&User{}, 20).Error)

	rows, total, err := ListInvoiceFeeHistory(nil, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, rows, 3)
	assert.Equal(t, []int64{2, 3, 1}, []int64{rows[0].ID, rows[1].ID, rows[2].ID})
	assert.Equal(t, 10, rows[0].UserID)
	assert.Equal(t, "alice", rows[0].Username)
	assert.Equal(t, "Alice A", rows[0].DisplayName)
	assert.Equal(t, 20, rows[1].UserID)
	assert.Empty(t, rows[1].Username)
}
