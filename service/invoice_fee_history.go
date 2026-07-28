package service

import (
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// ListInvoiceFeeHistory returns a bounded fee-ledger page, optionally restricted to one owner.
func ListInvoiceFeeHistory(ownerID *int, page, pageSize int) (dto.InvoiceFeeHistoryPage, error) {
	records, total, err := model.ListInvoiceFeeHistory(ownerID, (page-1)*pageSize, pageSize)
	if err != nil {
		return dto.InvoiceFeeHistoryPage{}, err
	}
	items := make([]dto.InvoiceFeeHistoryItem, 0, len(records))
	for _, record := range records {
		var before, after *int64
		if record.BalanceBefore != nil {
			value := int64(*record.BalanceBefore)
			before = &value
		}
		if record.BalanceAfter != nil {
			value := int64(*record.BalanceAfter)
			after = &value
		}
		items = append(items, dto.InvoiceFeeHistoryItem{
			ID: record.ID, ApplicationID: record.ApplicationID, ApplicationNo: record.ApplicationNo,
			EntryType: record.EntryType, FeePercent: record.FeePercent, Quota: int64(record.Quota),
			BalanceBefore: before, BalanceAfter: after, Status: record.Status, AppliedAt: record.AppliedAt,
		})
		if ownerID == nil {
			items[len(items)-1].UserID = &record.UserID
			items[len(items)-1].Username = &record.Username
			items[len(items)-1].DisplayName = &record.DisplayName
		}
	}
	return dto.InvoiceFeeHistoryPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}
