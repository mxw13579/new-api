package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func GetInvoiceConfig() dto.InvoiceConfig {
	setting := operation_setting.GetInvoiceSetting()
	return dto.InvoiceConfig{
		PersonalEnabled: setting.PersonalEnabled, CompanyEnabled: setting.CompanyEnabled,
		ApplicationWindowDays: setting.ApplicationWindowDays, MinimumAmountMinor: setting.MinimumAmountMinor,
		FeeQuota: setting.FeeQuota, PDFRetentionDays: setting.PDFRetentionDays, Currency: constant.InvoiceCurrencyCNY,
	}
}

func invoiceProfileDTO(profile *model.InvoiceProfile) *dto.InvoiceProfile {
	if profile == nil {
		return nil
	}
	return &dto.InvoiceProfile{
		ID: profile.ID, Type: profile.Type, Title: profile.Title, TaxNumber: profile.TaxNumber,
		IsDefault: profile.IsDefault, Version: profile.Version, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
	}
}

func CreateInvoiceProfile(userID int, request dto.CreateInvoiceProfileRequest) (*dto.InvoiceProfile, error) {
	profile, err := model.CreateInvoiceProfile(userID, request)
	if err != nil {
		return nil, err
	}
	return invoiceProfileDTO(profile), nil
}

func UpdateInvoiceProfile(userID int, request dto.UpdateInvoiceProfileRequest) (*dto.InvoiceProfile, error) {
	profile, err := model.UpdateInvoiceProfile(userID, request)
	if err != nil {
		return nil, err
	}
	return invoiceProfileDTO(profile), nil
}

func DeleteInvoiceProfile(userID int, request dto.DeleteInvoiceProfileRequest) error {
	return model.DeleteInvoiceProfile(userID, request)
}

func ListInvoiceProfiles(userID int) ([]dto.InvoiceProfile, error) {
	profiles, err := model.ListInvoiceProfiles(userID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.InvoiceProfile, 0, len(profiles))
	for i := range profiles {
		result = append(result, *invoiceProfileDTO(&profiles[i]))
	}
	return result, nil
}

func CreateInvoiceApplication(userID int, request dto.CreateInvoiceApplicationRequest) (*model.InvoiceApplication, error) {
	return model.CreateInvoiceApplication(userID, request, model.NewTopUpInvoicePaymentSource())
}

func CancelInvoiceApplication(userID int, applicationID int64) (*model.InvoiceApplication, error) {
	return model.CancelInvoiceApplication(userID, applicationID)
}

func ReviewInvoiceApplication(actorID int, applicationID int64, request dto.ReviewInvoiceApplicationRequest) (*model.InvoiceApplication, error) {
	targetStatus := constant.InvoiceApplicationStatusReviewing
	if request.Action == "approve" {
		targetStatus = constant.InvoiceApplicationStatusApproved
	} else if request.Action != "reviewing" {
		return nil, model.ErrInvoiceStateConflict
	}
	return model.TransitionInvoiceApplicationReview(actorID, applicationID, request.ExpectedStatus, targetStatus)
}

func RejectInvoiceApplication(actorID int, applicationID int64, request dto.RejectInvoiceApplicationRequest) (*model.InvoiceApplication, error) {
	return model.RejectInvoiceApplication(actorID, applicationID, request.ExpectedStatus, request.Reason)
}
