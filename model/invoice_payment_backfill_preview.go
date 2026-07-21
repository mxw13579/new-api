package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const invoicePaymentEvidencePolicyVersionIndex = "idx_invoice_payment_evidence_backfill_runs_policy_version"

type invoicePaymentEvidencePreviewAccumulator struct {
	exclusions     map[string]int64
	candidateCount int64
	amountMinor    int64
}

func validInvoicePaymentEvidenceSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

func validInvoicePaymentEvidenceAttestation(attestation InvoicePaymentEvidencePreviewAttestation) bool {
	return validInvoicePaymentEvidenceSHA(attestation.DeploymentSHA) && attestation.ActiveInstanceCount > 0 &&
		attestation.MatchingInstanceCount == attestation.ActiveInstanceCount && attestation.DisallowedInstanceCount == 0
}

func findInvoicePaymentEvidenceRunByPolicy(db *gorm.DB) (*InvoicePaymentEvidenceBackfillRun, error) {
	var run InvoicePaymentEvidenceBackfillRun
	if err := db.Where("policy_version = ?", constant.InvoicePaymentEvidencePolicyVersion).First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if run.CanonicalPolicyJSON != constant.InvoicePaymentEvidenceCanonicalPolicyJSON || run.PolicySHA256 != constant.InvoicePaymentEvidencePolicySHA256 {
		return nil, ErrInvoicePaymentEvidencePolicyMismatch
	}
	return &run, nil
}

func isInvoicePaymentEvidencePolicyVersionConflict(err error) bool {
	if err == nil {
		return false
	}
	var postgresErr *pgconn.PgError
	if errors.As(err, &postgresErr) {
		return postgresErr.Code == "23505" && postgresErr.ConstraintName == invoicePaymentEvidencePolicyVersionIndex
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062 && strings.Contains(strings.ToLower(mysqlErr.Message), invoicePaymentEvidencePolicyVersionIndex)
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed: invoice_payment_evidence_backfill_runs.policy_version")
}

func PreviewInvoicePaymentEvidenceBackfill(ctx context.Context, actorID int, attestation InvoicePaymentEvidencePreviewAttestation) (*InvoicePaymentEvidenceBackfillRun, bool, error) {
	if ctx == nil || actorID <= 0 || !validInvoicePaymentEvidenceAttestation(attestation) {
		return nil, false, ErrInvoicePaymentEvidenceInvalidRequest
	}
	if err := validateInvoicePaymentEvidencePolicy(); err != nil {
		return nil, false, err
	}
	if winner, err := findInvoicePaymentEvidenceRunByPolicy(DB.WithContext(ctx)); err != nil || winner != nil {
		return winner, false, err
	}
	var createdRun *InvoicePaymentEvidenceBackfillRun
	err := runInvoicePaymentEvidenceTransaction(ctx, DB, func(tx *gorm.DB) error {
		return previewInvoicePaymentEvidenceTx(ctx, tx, actorID, attestation, &createdRun)
	})
	if err == nil {
		return createdRun, true, nil
	}
	if !isInvoicePaymentEvidencePolicyVersionConflict(err) {
		return nil, false, err
	}
	winner, loadErr := findInvoicePaymentEvidenceRunByPolicy(DB.WithContext(ctx))
	if loadErr != nil || winner != nil {
		return winner, false, loadErr
	}
	return nil, false, err
}

func previewInvoicePaymentEvidenceTx(ctx context.Context, tx *gorm.DB, actorID int, attestation InvoicePaymentEvidencePreviewAttestation, createdRun **InvoicePaymentEvidenceBackfillRun) error {
	now := common.GetTimestamp()
	run := &InvoicePaymentEvidenceBackfillRun{
		PolicyVersion: constant.InvoicePaymentEvidencePolicyVersion, CanonicalPolicyJSON: constant.InvoicePaymentEvidenceCanonicalPolicyJSON,
		PolicySHA256: constant.InvoicePaymentEvidencePolicySHA256, Status: "previewing", ActorID: actorID, CreatedAt: now,
		PreviewExclusionReasons: "{}", CutoverAuditJSON: "{}",
	}
	if err := tx.Create(run).Error; err != nil {
		return err
	}
	cutoff, err := latestInvoicePaymentEvidenceTopUpID(tx)
	if err != nil {
		return err
	}
	run.CutoffMaxTopUpID = cutoff
	accumulator := invoicePaymentEvidencePreviewAccumulator{exclusions: map[string]int64{}}
	if err := scanInvoicePaymentEvidenceCandidates(ctx, tx, run, now, &accumulator); err != nil {
		return err
	}
	gapCount, err := countInvoicePaymentEvidenceGap(tx, run.ID)
	if err != nil {
		return err
	}
	if gapCount != 0 {
		return ErrInvoicePaymentEvidenceCutoverNotReady
	}
	if err := finalizeInvoicePaymentEvidencePreview(tx, run, attestation, now, accumulator); err != nil {
		return err
	}
	*createdRun = run
	return nil
}

func latestInvoicePaymentEvidenceTopUpID(tx *gorm.DB) (int, error) {
	var latest TopUp
	if err := tx.Order("id desc").First(&latest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return latest.Id, nil
}

func scanInvoicePaymentEvidenceCandidates(ctx context.Context, tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, now int64, accumulator *invoicePaymentEvidencePreviewAccumulator) error {
	cursor := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var topUps []TopUp
		if err := tx.Where("id > ? AND id <= ?", cursor, run.CutoffMaxTopUpID).Order("id asc").
			Limit(invoicePaymentEvidencePreviewBatchSize).Find(&topUps).Error; err != nil {
			return err
		}
		if len(topUps) == 0 {
			return nil
		}
		tradeNos := make([]string, 0, len(topUps))
		for i := range topUps {
			if !invoicePaymentEvidenceHasExistingEvidence(&topUps[i]) && topUps[i].PaymentProvider == PaymentProviderEpay && topUps[i].Status == common.TopUpStatusSuccess {
				tradeNos = append(tradeNos, topUps[i].TradeNo)
			}
		}
		subscriptionTrades, err := invoicePaymentEvidenceSubscriptionTradeSet(tx, tradeNos)
		if err != nil {
			return err
		}
		for i := range topUps {
			if err := addInvoicePaymentEvidencePreviewItem(tx, run.ID, &topUps[i], now, subscriptionTrades, accumulator); err != nil {
				return err
			}
		}
		cursor = topUps[len(topUps)-1].Id
	}
}

func addInvoicePaymentEvidencePreviewItem(tx *gorm.DB, runID int64, topUp *TopUp, now int64, subscriptionTrades map[string]struct{}, accumulator *invoicePaymentEvidencePreviewAccumulator) error {
	reason, fingerprint, amount, err := invoicePaymentEvidenceCandidate(topUp, subscriptionTrades)
	if err != nil {
		return err
	}
	if reason != "" {
		accumulator.exclusions[reason], err = checkedAddInt64(accumulator.exclusions[reason], 1)
		return err
	}
	accumulator.candidateCount, err = checkedAddInt64(accumulator.candidateCount, 1)
	if err != nil {
		return err
	}
	accumulator.amountMinor, err = checkedAddInt64(accumulator.amountMinor, amount)
	if err != nil {
		return err
	}
	item := InvoicePaymentEvidenceBackfillItem{RunID: runID, TopUpID: topUp.Id, ExpectedAmountMinor: amount, SourceFingerprint: fingerprint, CreatedAt: now}
	if err := tx.Create(&item).Error; err != nil {
		return err
	}
	return nil
}

func countInvoicePaymentEvidenceGap(tx *gorm.DB, runID int64) (int64, error) {
	cursor := 0
	var gapCount int64
	for {
		var topUps []TopUp
		if err := tx.Where("id > ?", cursor).Order("id asc").Limit(invoicePaymentEvidencePreviewBatchSize).Find(&topUps).Error; err != nil {
			return 0, err
		}
		if len(topUps) == 0 {
			return gapCount, nil
		}
		tradeNos := make([]string, 0, len(topUps))
		for i := range topUps {
			if !invoicePaymentEvidenceHasExistingEvidence(&topUps[i]) && topUps[i].PaymentProvider == PaymentProviderEpay && topUps[i].Status == common.TopUpStatusSuccess {
				tradeNos = append(tradeNos, topUps[i].TradeNo)
			}
		}
		subscriptionTrades, err := invoicePaymentEvidenceSubscriptionTradeSet(tx, tradeNos)
		if err != nil {
			return 0, err
		}
		candidateIDs := make([]int, 0, len(topUps))
		for i := range topUps {
			reason, _, _, candidateErr := invoicePaymentEvidenceCandidate(&topUps[i], subscriptionTrades)
			if candidateErr != nil {
				return 0, candidateErr
			}
			if reason == "" {
				candidateIDs = append(candidateIDs, topUps[i].Id)
			}
		}
		var representedIDs []int
		if len(candidateIDs) != 0 {
			if err := tx.Model(&InvoicePaymentEvidenceBackfillItem{}).Where("run_id = ? AND topup_id IN ?", runID, candidateIDs).
				Pluck("topup_id", &representedIDs).Error; err != nil {
				return 0, err
			}
		}
		represented := make(map[int]struct{}, len(representedIDs))
		for _, topUpID := range representedIDs {
			represented[topUpID] = struct{}{}
		}
		for _, topUpID := range candidateIDs {
			if _, exists := represented[topUpID]; exists {
				continue
			}
			gapCount, err = checkedAddInt64(gapCount, 1)
			if err != nil {
				return 0, err
			}
		}
		cursor = topUps[len(topUps)-1].Id
	}
}

func finalizeInvoicePaymentEvidencePreview(tx *gorm.DB, run *InvoicePaymentEvidenceBackfillRun, attestation InvoicePaymentEvidencePreviewAttestation, now int64, accumulator invoicePaymentEvidencePreviewAccumulator) error {
	exclusionJSON, err := common.Marshal(accumulator.exclusions)
	if err != nil {
		return err
	}
	auditJSON, err := common.Marshal(InvoicePaymentEvidenceCutoverAudit{
		SchemaVersion: 1, DeploymentSHA: attestation.DeploymentSHA, ActiveInstanceCount: attestation.ActiveInstanceCount,
		MatchingInstanceCount: attestation.MatchingInstanceCount, DisallowedInstanceCount: attestation.DisallowedInstanceCount,
		PostPreviewUnversionedSuccessCount: 0,
	})
	if err != nil {
		return err
	}
	run.PreviewCandidateCount, run.PreviewAmountMinor = accumulator.candidateCount, accumulator.amountMinor
	run.PreviewExclusionReasons, run.CutoverAuditJSON = string(exclusionJSON), string(auditJSON)
	run.Status, run.PreviewedAt = InvoicePaymentEvidenceRunStatusPreviewed, &now
	return tx.Save(run).Error
}
