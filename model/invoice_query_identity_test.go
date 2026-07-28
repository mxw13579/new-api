package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestListInvoiceUserIdentitiesUsesOneBoundedLookup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}))
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
	require.NoError(t, db.Create(&[]User{{Id: 10, Username: "alice", DisplayName: "Alice A", AffCode: "aff-a"}, {Id: 20, Username: "bob", AffCode: "aff-b"}}).Error)

	identities, err := ListInvoiceUserIdentities([]int{20, 10, 20})
	require.NoError(t, err)
	assert.Equal(t, InvoiceUserIdentity{UserID: 10, Username: "alice", DisplayName: "Alice A"}, identities[10])
	assert.Equal(t, InvoiceUserIdentity{UserID: 20, Username: "bob"}, identities[20])
}
