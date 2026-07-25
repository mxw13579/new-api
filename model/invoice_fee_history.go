package model

import (
	"github.com/QuantumNous/new-api/common"
)

// InvoiceFeeHistoryRecord contains only owner-visible ledger and application facts.
type InvoiceFeeHistoryRecord struct {
	ID            int64
	ApplicationID int64
	ApplicationNo string
	EntryType     string
	FeePercent    int
	Quota         int
	BalanceBefore *int
	BalanceAfter  *int
	Status        string
	AppliedAt     *int64
}

// ListInvoiceFeeHistory returns one newest-first page for a single owner.
func ListInvoiceFeeHistory(userID, offset, limit int) ([]InvoiceFeeHistoryRecord, int64, error) {
	if userID <= 0 || offset < 0 || limit <= 0 || limit > 100 {
		return nil, 0, ErrInvoicePaymentSourceInvalidRequest
	}
	query := DB.Table("invoice_fee_ledger_entries AS fee").
		Joins("JOIN invoice_applications AS app ON app.id = fee.application_id AND app.user_id = fee.user_id").
		Where("fee.user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []struct {
		ID             int64
		ApplicationID  int64
		ApplicationNo  string
		EntryType      string
		Quota          int
		BalanceBefore  *int
		BalanceAfter   *int
		Status         string
		AppliedAt      *int64
		PolicySnapshot string
	}
	if err := query.Select("fee.id, fee.application_id, app.application_no, fee.entry_type, fee.quota, fee.balance_before, fee.balance_after, fee.status, fee.applied_at, app.policy_snapshot").
		Order("fee.created_at DESC, fee.id DESC").Offset(offset).Limit(limit).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]InvoiceFeeHistoryRecord, 0, len(rows))
	for _, row := range rows {
		var policy struct {
			FeePercent int `json:"fee_percent"`
		}
		if err := common.UnmarshalJsonStr(row.PolicySnapshot, &policy); err != nil {
			return nil, 0, err
		}
		result = append(result, InvoiceFeeHistoryRecord{
			ID: row.ID, ApplicationID: row.ApplicationID, ApplicationNo: row.ApplicationNo,
			EntryType: row.EntryType, FeePercent: policy.FeePercent, Quota: row.Quota,
			BalanceBefore: row.BalanceBefore, BalanceAfter: row.BalanceAfter, Status: row.Status, AppliedAt: row.AppliedAt,
		})
	}
	return result, total, nil
}
