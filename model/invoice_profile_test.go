package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInvoiceProfileTest(t *testing.T) int {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&InvoiceProfile{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM invoice_profiles") })
	user := User{Username: "invoice-profile-user", Password: "password", Quota: 1000}
	require.NoError(t, DB.Create(&user).Error)
	return user.Id
}

func TestInvoiceProfileValidationAndCreationVersion(t *testing.T) {
	userID := setupInvoiceProfileTest(t)

	personal, err := CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: " Alice ", IdentityCardNumber: " 11010519491231002X ", IsDefault: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "Alice", personal.Title)
	assert.Empty(t, personal.TaxNumber)
	assert.Equal(t, "11010519491231002X", personal.IdentityCardNumber)
	assert.Equal(t, int64(1), personal.Version)

	_, err = CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: "Alice", TaxNumber: "must-not-exist", IdentityCardNumber: "11010519491231002X",
	})
	assert.ErrorIs(t, err, ErrInvoiceInvalidProfile)

	_, err = CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypeCompany, Title: "Example Ltd", IdentityCardNumber: "11010519491231002X",
	})
	assert.ErrorIs(t, err, ErrInvoiceInvalidProfile)

	company, err := CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypeCompany, Title: " Example Ltd ", TaxNumber: " 91300000TEST ",
	})
	require.NoError(t, err)
	assert.Equal(t, "Example Ltd", company.Title)
	assert.Equal(t, "91300000TEST", company.TaxNumber)
	assert.Equal(t, int64(1), company.Version)
}

func TestInvoiceProfileExpectedVersionAndDefaultConvergence(t *testing.T) {
	userID := setupInvoiceProfileTest(t)
	first, err := CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: "First", IsDefault: true,
	})
	require.NoError(t, err)
	second, err := CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: "Second",
	})
	require.NoError(t, err)
	third, err := CreateInvoiceProfile(userID, dto.CreateInvoiceProfileRequest{
		Type: constant.InvoiceTypePersonal, Title: "Third",
	})
	require.NoError(t, err)

	_, err = UpdateInvoiceProfile(userID, dto.UpdateInvoiceProfileRequest{
		ID: first.ID, ExpectedVersion: first.Version + 1, Title: "stale", IsDefault: true,
	})
	assert.ErrorIs(t, err, ErrInvoiceProfileVersionConflict)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, profile := range []*InvoiceProfile{second, third} {
		profile := profile
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, updateErr := UpdateInvoiceProfile(userID, dto.UpdateInvoiceProfileRequest{
				ID: profile.ID, ExpectedVersion: profile.Version, Title: profile.Title, IsDefault: true,
			})
			errs <- updateErr
		}()
	}
	wg.Wait()
	close(errs)
	for updateErr := range errs {
		require.NoError(t, updateErr)
	}

	var profiles []InvoiceProfile
	require.NoError(t, DB.Where("user_id = ? AND type = ?", userID, constant.InvoiceTypePersonal).Order("id").Find(&profiles).Error)
	defaultCount := 0
	for _, profile := range profiles {
		if profile.IsDefault {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount)

	var demoted InvoiceProfile
	require.NoError(t, DB.First(&demoted, first.ID).Error)
	assert.False(t, demoted.IsDefault)
	assert.Equal(t, int64(2), demoted.Version, "implicit default displacement increments once")
}
