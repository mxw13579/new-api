package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ListEligibleInvoiceOrders returns a bounded page of the user's paid orders that remain eligible for invoicing.
func ListEligibleInvoiceOrders(userID, page, pageSize int) (dto.EligibleInvoiceOrderPage, error) {
	setting := operation_setting.GetInvoiceSetting()
	cutoff := time.Now().Unix() - int64(setting.ApplicationWindowDays)*86400
	topUps, err := model.ListEligibleInvoiceTopUps(userID, cutoff)
	if err != nil {
		return dto.EligibleInvoiceOrderPage{}, err
	}
	total := int64(len(topUps))
	start := (page - 1) * pageSize
	if start > len(topUps) {
		start = len(topUps)
	}
	end := start + pageSize
	if end > len(topUps) {
		end = len(topUps)
	}
	items := make([]dto.EligibleInvoiceOrder, 0, end-start)
	for _, topUp := range topUps[start:end] {
		items = append(items, dto.EligibleInvoiceOrder{
			TopUpID: topUp.Id, OrderNo: topUp.TradeNo, PaidAmountMinor: *topUp.PaidAmountMinor,
			Currency: *topUp.Currency, ProductDescription: *topUp.ProductSnapshot, PaidAt: topUp.CompleteTime,
		})
	}
	return dto.EligibleInvoiceOrderPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func invoiceApplicationSummary(application *model.InvoiceApplication, document *model.InvoiceDocument) dto.InvoiceApplicationSummary {
	return invoiceApplicationSummaryAt(application, document, time.Now())
}

func invoiceApplicationSummaryAt(application *model.InvoiceApplication, document *model.InvoiceDocument, now time.Time) dto.InvoiceApplicationSummary {
	documentStatus := constant.InvoiceDocumentStatusMissing
	var expiresAt, deletedAt *int64
	if document != nil {
		documentStatus = document.Status
		expiresAt = document.ExpiresAt
		deletedAt = document.DeletedAt
	}
	_, downloadErr := invoiceDocumentDownloadTTL(application, document, now)
	return dto.InvoiceApplicationSummary{
		ID: application.ID, ApplicationNo: application.ApplicationNo, Type: application.Type,
		Status: application.Status, PaymentReviewStatus: application.PaymentReviewStatus,
		Currency: application.Currency, AmountMinor: application.AmountMinor, FeeQuota: int64(application.FeeQuota),
		FeeStatus: application.FeeStatus, SubmittedAt: application.SubmittedAt, ReviewedAt: application.ReviewedAt,
		CancelledAt: application.CancelledAt, IssuedAt: application.IssuedAt, RejectReason: application.RejectReason,
		DocumentStatus: documentStatus, DocumentExpiresAt: expiresAt, DocumentDeletedAt: deletedAt,
		CanCancel:   application.Status == constant.InvoiceApplicationStatusSubmitted,
		CanDownload: downloadErr == nil,
	}
}

// ListInvoiceApplicationPage returns a bounded owner-scoped or administrative page of invoice summaries.
func ListInvoiceApplicationPage(ownerID *int, page, pageSize int) (dto.InvoiceApplicationPage, error) {
	applications, total, err := model.ListInvoiceApplications(ownerID, (page-1)*pageSize, pageSize)
	if err != nil {
		return dto.InvoiceApplicationPage{}, err
	}
	items := make([]dto.InvoiceApplicationSummary, 0, len(applications))
	for i := range applications {
		document, err := model.GetInvoiceDocument(applications[i].ActiveDocumentID)
		if err != nil {
			return dto.InvoiceApplicationPage{}, err
		}
		items = append(items, invoiceApplicationSummary(&applications[i], document))
	}
	return dto.InvoiceApplicationPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

// GetInvoiceApplicationDetail returns immutable invoice facts while masking sensitive profile data unless authorized.
func GetInvoiceApplicationDetail(applicationID int64, ownerID *int, includeSensitive bool) (*dto.InvoiceApplicationDetail, error) {
	application, err := model.GetInvoiceApplication(applicationID, ownerID)
	if err != nil {
		return nil, err
	}
	items, err := model.GetInvoiceApplicationItems(application.ID)
	if err != nil {
		return nil, err
	}
	issuance, err := model.GetInvoiceIssuance(application.ID)
	if err != nil {
		return nil, err
	}
	document, err := model.GetInvoiceDocument(application.ActiveDocumentID)
	if err != nil {
		return nil, err
	}
	profile := dto.InvoiceProfileSnapshot{}
	if err := common.UnmarshalJsonStr(application.ProfileSnapshot, &profile); err != nil {
		return nil, err
	}
	if !includeSensitive {
		profile.Title = ""
		profile.TaxNumber = ""
	}
	policy := dto.InvoicePolicySnapshot{}
	if err := common.UnmarshalJsonStr(application.PolicySnapshot, &policy); err != nil {
		return nil, err
	}
	detail := &dto.InvoiceApplicationDetail{
		InvoiceApplicationSummary: invoiceApplicationSummary(application, document),
		ProfileSnapshot:           profile, PolicySnapshot: policy,
		Items: make([]dto.InvoiceApplicationItem, 0, len(items)),
	}
	for _, item := range items {
		detail.Items = append(detail.Items, dto.InvoiceApplicationItem{
			TopUpID: item.TopUpID, OrderNo: item.TradeNo, PaidAmountMinor: item.PaidAmountMinor,
			Currency: item.Currency, ProductDescription: item.ProductDescription, PaidAt: item.PaidAt,
		})
	}
	if issuance != nil {
		detail.Issuance = &dto.InvoiceIssuanceMetadata{
			ID: issuance.ID, InvoiceNumber: issuance.InvoiceNumber, InvoiceCode: issuance.InvoiceCode,
			InvoiceDate: issuance.InvoiceDate, FaceAmountMinor: issuance.FaceAmountMinor, Currency: issuance.Currency,
		}
	}
	if document != nil {
		detail.Document = &dto.InvoiceDocumentMetadata{ID: document.ID, Status: document.Status, ExpiresAt: document.ExpiresAt, DeletedAt: document.DeletedAt}
	}
	return detail, nil
}

// CreateInvoiceApplicationDetail creates an application and returns its owner-visible detail projection.
func CreateInvoiceApplicationDetail(userID int, request dto.CreateInvoiceApplicationRequest) (*dto.InvoiceApplicationDetail, error) {
	application, err := CreateInvoiceApplication(userID, request)
	if err != nil {
		return nil, err
	}
	return GetInvoiceApplicationDetail(application.ID, &userID, true)
}

// CancelInvoiceApplicationDetail cancels an application and returns its resulting owner-visible detail projection.
func CancelInvoiceApplicationDetail(userID int, applicationID int64) (*dto.InvoiceApplicationDetail, error) {
	application, err := CancelInvoiceApplication(userID, applicationID)
	if err != nil {
		return nil, err
	}
	return GetInvoiceApplicationDetail(application.ID, &userID, true)
}
