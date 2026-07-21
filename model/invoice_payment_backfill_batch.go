package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type InvoicePaymentEvidenceApplyBatchResult struct {
	ProcessedCount int64
	LastTopUpID    int
	HasMore        bool
}

func ApplyInvoicePaymentEvidenceBatchTx(tx *gorm.DB, runID int64) (InvoicePaymentEvidenceApplyBatchResult, error) {
	if tx == nil || runID <= 0 {
		return InvoicePaymentEvidenceApplyBatchResult{}, ErrInvoicePaymentEvidenceInvalidRequest
	}
	var run InvoicePaymentEvidenceBackfillRun
	if err := lockForUpdate(tx).First(&run, runID).Error; err != nil {
		return InvoicePaymentEvidenceApplyBatchResult{}, err
	}
	if run.Status != InvoicePaymentEvidenceRunStatusApplying {
		return InvoicePaymentEvidenceApplyBatchResult{}, ErrInvoicePaymentEvidenceRunStateConflict
	}
	var items []InvoicePaymentEvidenceBackfillItem
	if err := tx.Where("run_id = ? AND topup_id > ?", run.ID, run.CursorTopUpID).Order("topup_id asc").
		Limit(invoicePaymentEvidencePreviewBatchSize).Find(&items).Error; err != nil {
		return InvoicePaymentEvidenceApplyBatchResult{}, err
	}
	for i := range items {
		if err := applyInvoicePaymentEvidenceItem(tx, &run, &items[i]); err != nil {
			return InvoicePaymentEvidenceApplyBatchResult{}, err
		}
	}
	return advanceInvoicePaymentEvidenceCursor(tx, &run, items)
}

func applyInvoicePaymentEvidenceItem(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, item *InvoicePaymentEvidenceBackfillItem) error {
	if item.RunID != run.ID || item.TopUpID <= 0 || item.TopUpID > run.CutoffMaxTopUpID {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	var topUp TopUp
	if err := lockForUpdate(tx).First(&topUp, item.TopUpID).Error; err != nil {
		return err
	}
	if exactLegacyInvoicePaymentEvidence(&topUp, item, run.ID) {
		return validateLegacyInvoicePaymentEvidenceSubscription(tx, &topUp)
	}
	reason, fingerprint, amount, err := invoicePaymentEvidenceCandidate(tx, &topUp)
	if err != nil {
		return err
	}
	if reason != "" || fingerprint != item.SourceFingerprint || amount != item.ExpectedAmountMinor {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	return writeLegacyInvoicePaymentEvidence(tx, &topUp, item, run.ID)
}

func exactLegacyInvoicePaymentEvidence(topUp *TopUp, item *InvoicePaymentEvidenceBackfillItem, runID int64) bool {
	if topUp.Status != common.TopUpStatusSuccess || topUp.InvoiceEligible == nil || !*topUp.InvoiceEligible ||
		topUp.PaidAmountMinor == nil || *topUp.PaidAmountMinor != item.ExpectedAmountMinor ||
		topUp.Currency == nil || *topUp.Currency != constant.InvoicePaymentEvidenceCurrencyCNY ||
		topUp.PaymentState == nil || *topUp.PaymentState != constant.InvoicePaymentStateSucceeded ||
		topUp.RefundedAmountMinor == nil || *topUp.RefundedAmountMinor != 0 || topUp.PaymentVersion == nil || *topUp.PaymentVersion != 1 ||
		topUp.ProductSnapshot == nil || *topUp.ProductSnapshot != constant.InvoicePaymentEvidenceTopUpProduct ||
		topUp.PaymentEvidenceSource == nil || *topUp.PaymentEvidenceSource != constant.InvoicePaymentEvidenceSourceLegacyBackfill ||
		topUp.PaymentEvidenceRunID == nil || *topUp.PaymentEvidenceRunID != runID || topUp.InvoiceApplicationID != nil ||
		topUp.PaymentProviderTradeNo != nil || topUp.PaymentProviderTradeKey != nil {
		return false
	}
	fingerprint, amount, err := invoicePaymentEvidenceFingerprint(topUp)
	return err == nil && amount == item.ExpectedAmountMinor && fingerprint == item.SourceFingerprint
}

func validateLegacyInvoicePaymentEvidenceSubscription(tx *gorm.DB, topUp *TopUp) error {
	var count int64
	if err := tx.Model(&SubscriptionOrder{}).Where("trade_no = ?", topUp.TradeNo).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	return nil
}

func writeLegacyInvoicePaymentEvidence(tx *gorm.DB, topUp *TopUp, item *InvoicePaymentEvidenceBackfillItem, runID int64) error {
	update := tx.Model(&TopUp{}).Where(`id = ? AND (payment_version IS NULL OR payment_version = 0)
AND invoice_application_id IS NULL AND paid_amount_minor IS NULL AND currency IS NULL AND invoice_eligible IS NULL
AND payment_state IS NULL AND refunded_amount_minor IS NULL AND product_snapshot IS NULL
AND payment_evidence_source IS NULL AND payment_evidence_run_id IS NULL
AND payment_provider_trade_no IS NULL AND payment_provider_trade_key IS NULL`, topUp.Id).
		Updates(map[string]any{
			"paid_amount_minor": item.ExpectedAmountMinor, "currency": constant.InvoicePaymentEvidenceCurrencyCNY,
			"invoice_eligible": true, "payment_state": constant.InvoicePaymentStateSucceeded, "refunded_amount_minor": int64(0),
			"payment_version": int64(1), "product_snapshot": constant.InvoicePaymentEvidenceTopUpProduct,
			"payment_evidence_source": constant.InvoicePaymentEvidenceSourceLegacyBackfill, "payment_evidence_run_id": runID,
		})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected == 1 {
		return nil
	}
	var current TopUp
	if err := tx.First(&current, topUp.Id).Error; err != nil || !exactLegacyInvoicePaymentEvidence(&current, item, runID) {
		return ErrInvoicePaymentSourceEvidenceConflict
	}
	return validateLegacyInvoicePaymentEvidenceSubscription(tx, &current)
}

func advanceInvoicePaymentEvidenceCursor(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, items []InvoicePaymentEvidenceBackfillItem) (InvoicePaymentEvidenceApplyBatchResult, error) {
	result := InvoicePaymentEvidenceApplyBatchResult{LastTopUpID: run.CursorTopUpID, ProcessedCount: int64(len(items))}
	if len(items) == 0 {
		return result, nil
	}
	result.LastTopUpID = items[len(items)-1].TopUpID
	update := tx.Model(&InvoicePaymentEvidenceBackfillRun{}).
		Where("id = ? AND status = ? AND cursor_top_up_id = ?", run.ID, InvoicePaymentEvidenceRunStatusApplying, run.CursorTopUpID).
		Update("cursor_top_up_id", result.LastTopUpID)
	if update.Error != nil || update.RowsAffected != 1 {
		if update.Error != nil {
			return InvoicePaymentEvidenceApplyBatchResult{}, update.Error
		}
		return InvoicePaymentEvidenceApplyBatchResult{}, ErrInvoicePaymentEvidenceRunStateConflict
	}
	var remaining int64
	if err := tx.Model(&InvoicePaymentEvidenceBackfillItem{}).Where("run_id = ? AND topup_id > ?", run.ID, result.LastTopUpID).Count(&remaining).Error; err != nil {
		return InvoicePaymentEvidenceApplyBatchResult{}, err
	}
	result.HasMore = remaining != 0
	return result, nil
}

func ReconcileInvoicePaymentEvidenceBackfillTx(tx *gorm.DB, runID int64) error {
	if tx == nil || runID <= 0 {
		return ErrInvoicePaymentEvidenceInvalidRequest
	}
	var run InvoicePaymentEvidenceBackfillRun
	if err := lockForUpdate(tx).First(&run, runID).Error; err != nil {
		return err
	}
	if run.Status != InvoicePaymentEvidenceRunStatusApplying {
		return ErrInvoicePaymentEvidenceRunStateConflict
	}
	items, itemCount, itemAmount, err := invoicePaymentEvidenceItemTruth(tx, run.ID)
	if err != nil || itemCount != run.PreviewCandidateCount || itemAmount != run.PreviewAmountMinor {
		return errors.Join(ErrInvoicePaymentSourceEvidenceConflict, err)
	}
	legacyCount, legacyAmount, err := invoicePaymentEvidenceLegacyTruth(tx, run.ID, items)
	if err != nil || legacyCount != itemCount || legacyAmount != itemAmount {
		return errors.Join(ErrInvoicePaymentSourceEvidenceConflict, err)
	}
	return reconcileInvoicePaymentEvidenceGap(tx, items)
}

func invoicePaymentEvidenceItemTruth(tx *gorm.DB, runID int64) (map[int]InvoicePaymentEvidenceBackfillItem, int64, int64, error) {
	var rows []InvoicePaymentEvidenceBackfillItem
	if err := tx.Where("run_id = ?", runID).Order("topup_id asc").Find(&rows).Error; err != nil {
		return nil, 0, 0, err
	}
	items := make(map[int]InvoicePaymentEvidenceBackfillItem, len(rows))
	var amount int64
	for _, item := range rows {
		var err error
		amount, err = checkedAddInt64(amount, item.ExpectedAmountMinor)
		if err != nil {
			return nil, 0, 0, err
		}
		items[item.TopUpID] = item
	}
	return items, int64(len(rows)), amount, nil
}

func invoicePaymentEvidenceLegacyTruth(tx *gorm.DB, runID int64, items map[int]InvoicePaymentEvidenceBackfillItem) (int64, int64, error) {
	var rows []TopUp
	if err := tx.Where("payment_evidence_source = ? AND payment_evidence_run_id = ?", constant.InvoicePaymentEvidenceSourceLegacyBackfill, runID).
		Order("id asc").Find(&rows).Error; err != nil {
		return 0, 0, err
	}
	var count, amount int64
	for i := range rows {
		item, ok := items[rows[i].Id]
		if !ok || !exactLegacyInvoicePaymentEvidence(&rows[i], &item, runID) {
			return 0, 0, ErrInvoicePaymentSourceEvidenceConflict
		}
		if err := validateLegacyInvoicePaymentEvidenceSubscription(tx, &rows[i]); err != nil {
			return 0, 0, err
		}
		var err error
		count, err = checkedAddInt64(count, 1)
		if err == nil {
			amount, err = checkedAddInt64(amount, *rows[i].PaidAmountMinor)
		}
		if err != nil {
			return 0, 0, err
		}
	}
	return count, amount, nil
}

func reconcileInvoicePaymentEvidenceGap(tx *gorm.DB, items map[int]InvoicePaymentEvidenceBackfillItem) error {
	var rows []TopUp
	if err := tx.Order("id asc").Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		if rows[i].PaymentVersion != nil && *rows[i].PaymentVersion != 0 {
			continue
		}
		reason, _, _, err := invoicePaymentEvidenceCandidate(tx, &rows[i])
		if err != nil {
			return err
		}
		if _, represented := items[rows[i].Id]; reason == "" && !represented {
			return ErrInvoicePaymentEvidenceCutoverNotReady
		}
	}
	return nil
}
