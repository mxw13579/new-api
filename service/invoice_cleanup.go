package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	invoiceCleanupBatchSize         = 100
	invoiceCleanupStaleLimit        = invoiceCleanupBatchSize / 2
	invoiceCleanupLeaseAge          = 15 * time.Minute
	invoiceCleanupMaxDeleteAttempts = 16
)

// InvoiceDocumentCleanupResult summarizes bounded reconciliation and object-deletion work for one scheduled run.
type InvoiceDocumentCleanupResult struct {
	Reconciled int `json:"reconciled"`
	Processed  int `json:"processed"`
	Deleted    int `json:"deleted"`
	Failed     int `json:"failed"`
}

type invoiceDocumentCleanupHandler struct {
	db     *gorm.DB
	store  InvoiceObjectStore
	now    func() int64
	finish func(string, string, model.SystemTaskStatus, any, string) error
}

var _ ScheduledSystemTaskHandler = (*invoiceDocumentCleanupHandler)(nil)

// NewInvoiceDocumentCleanupHandler creates the hourly handler that reconciles stale uploads and expires stored PDFs.
func NewInvoiceDocumentCleanupHandler() SystemTaskHandler {
	return &invoiceDocumentCleanupHandler{db: model.DB, now: func() int64 { return time.Now().Unix() }, finish: model.FinishSystemTask}
}

// Type identifies invoice document cleanup tasks in the shared system-task registry.
func (*invoiceDocumentCleanupHandler) Type() string { return model.InvoiceDocumentCleanupTaskType }

// Enabled keeps invoice retention enforcement active on the system-task scheduler.
func (*invoiceDocumentCleanupHandler) Enabled() bool { return true }

// Interval returns the hourly cadence required by the invoice retention contract.
func (*invoiceDocumentCleanupHandler) Interval() time.Duration { return time.Hour }

// NewPayload returns the empty payload used by periodic invoice cleanup runs.
func (*invoiceDocumentCleanupHandler) NewPayload() any { return nil }

// Run reconciles and cleans a bounded invoice-document batch, then terminalizes the owning system task.
func (handler *invoiceDocumentCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if handler.finish == nil {
		handler.finish = model.FinishSystemTask
	}
	store := handler.store
	if store == nil {
		configured, err := NewInvoiceR2StoreFromEnvironment()
		if err != nil {
			handler.finishTask(ctx, task, runnerID, model.SystemTaskStatusFailed, InvoiceDocumentCleanupResult{}, "object_store_unavailable")
			return
		}
		store = configured
	}
	now := handler.now()
	staleBefore := now - int64(invoiceCleanupLeaseAge.Seconds())
	reconciled, err := ReconcileStaleInvoiceDocuments(ctx, handler.db, store, now, staleBefore, invoiceCleanupStaleLimit)
	result := InvoiceDocumentCleanupResult{Reconciled: reconciled}
	if err == nil && reconciled < invoiceCleanupBatchSize {
		cleanupResult, cleanupErr := CleanupInvoiceDocuments(ctx, handler.db, store, now, staleBefore, invoiceCleanupBatchSize-reconciled)
		result.Processed = cleanupResult.Processed
		result.Deleted = cleanupResult.Deleted
		result.Failed = cleanupResult.Failed
		err = cleanupErr
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, model.ErrSystemTaskLockLost) {
			return
		}
		handler.finishTask(ctx, task, runnerID, model.SystemTaskStatusFailed, result, "invoice_cleanup_failed")
		return
	}
	handler.finishTask(ctx, task, runnerID, model.SystemTaskStatusSucceeded, result, "")
}

func (handler *invoiceDocumentCleanupHandler) finishTask(ctx context.Context, task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result InvoiceDocumentCleanupResult, errorMessage string) {
	err := handler.finish(task.TaskID, runnerID, status, result, errorMessage)
	if err == nil || errors.Is(err, model.ErrSystemTaskLockLost) {
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf("invoice document cleanup finish persistence failed: task_id=%s", task.TaskID))
}

// ReconcileStaleInvoiceDocuments converges a bounded batch of expired upload leases without guessing object keys.
func ReconcileStaleInvoiceDocuments(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, now, staleBefore int64, limit int) (int, error) {
	if db == nil || store == nil || now <= 0 || staleBefore <= 0 || limit <= 0 {
		return 0, model.ErrInvoiceDocumentConflict
	}
	db = db.WithContext(ctx)
	var documents []model.InvoiceDocument
	if err := db.Where("(status IN ? AND operation_started_at <= ?) OR (status = ? AND last_recovery_error = ? AND last_recovery_at <= ?)", []string{
		model.InvoiceDocumentStatusUploading, model.InvoiceDocumentStatusValidating,
	}, staleBefore, model.InvoiceDocumentStatusUploadFailed, model.InvoiceDocumentRecoveryDeleteRetryable, staleBefore).
		Order("last_recovery_at asc").Order("id asc").Limit(limit).Find(&documents).Error; err != nil {
		return 0, err
	}
	processed := 0
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		if _, err := ReconcileInvoiceDocument(ctx, db, store, document.ID, now, staleBefore); err != nil {
			if errors.Is(err, model.ErrInvoiceDocumentConflict) {
				continue
			}
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// CleanupInvoiceDocuments claims and deletes a bounded batch of expired or superseded objects while retaining database records.
func CleanupInvoiceDocuments(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, now, staleBefore int64, limit int) (InvoiceDocumentCleanupResult, error) {
	result := InvoiceDocumentCleanupResult{}
	if db == nil || store == nil || now <= 0 || staleBefore <= 0 || limit <= 0 {
		return result, model.ErrInvoiceDocumentConflict
	}
	db = db.WithContext(ctx)
	documents, err := findInvoiceDocumentCleanupCandidates(db, now, staleBefore, limit)
	if err != nil {
		return result, err
	}
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		category := invoiceDocumentPrevalidationFailure(store, document)
		if category != "" {
			terminalized, terminalErr := terminalizeInvoiceDocumentCleanupCandidate(db, document, category, now)
			if terminalErr != nil {
				return result, terminalErr
			}
			if terminalized {
				result.Processed++
				result.Failed++
			}
			continue
		}
		token, claimed, claimErr := claimInvoiceDocumentCleanupCandidate(db, document, now)
		if claimErr != nil {
			return result, claimErr
		}
		if !claimed {
			continue
		}
		result.Processed++
		deleteErr := store.Delete(ctx, *document.ObjectKey)
		if err := ctx.Err(); err != nil {
			return result, err
		}
		deleted, finishErr := finishInvoiceDocumentCleanupCandidate(db, document, token, deleteErr, now)
		if finishErr != nil {
			return result, finishErr
		}
		if deleted {
			result.Deleted++
		} else {
			result.Failed++
		}
	}
	return result, nil
}

func findInvoiceDocumentCleanupCandidates(db *gorm.DB, now, staleBefore int64, limit int) ([]model.InvoiceDocument, error) {
	var documents []model.InvoiceDocument
	err := db.Where(
		"(status = ? AND expires_at IS NOT NULL AND expires_at <= ?) OR "+
			"(status = ? AND delete_error_category = ? AND expires_at IS NOT NULL AND expires_at <= ?) OR "+
			"status = ? OR "+
			"(status = ? AND (delete_error_category IS NULL OR delete_error_category = '' OR (delete_error_category = ? AND (next_delete_attempt_at IS NULL OR next_delete_attempt_at <= ?)))) OR "+
			"(status = ? AND operation_started_at <= ?)",
		model.InvoiceDocumentStatusAvailable, now,
		model.InvoiceDocumentStatusMissing, model.InvoiceDocumentDeleteErrorObjectIntegrityMismatch, now,
		model.InvoiceDocumentStatusSuperseded,
		model.InvoiceDocumentStatusDeleteFailed, model.InvoiceDocumentDeleteErrorRetryable, now,
		model.InvoiceDocumentStatusDeleting, staleBefore,
	).Order("id asc").Limit(limit).Find(&documents).Error
	return documents, err
}

func invoiceDocumentPrevalidationFailure(store InvoiceObjectStore, document model.InvoiceDocument) string {
	if document.ObjectKey == nil {
		return model.InvoiceDocumentDeleteErrorMissingObjectKey
	}
	if validateInvoiceObjectKey(*document.ObjectKey) != nil {
		return model.InvoiceDocumentDeleteErrorTerminal
	}
	if !invoiceObjectStoreMatchesBucket(store, document.R2Bucket) {
		return model.InvoiceDocumentDeleteErrorBucketMismatch
	}
	if document.DeleteAttempts >= invoiceCleanupMaxDeleteAttempts {
		return model.InvoiceDocumentDeleteErrorTerminal
	}
	return ""
}

func terminalizeInvoiceDocumentCleanupCandidate(db *gorm.DB, document model.InvoiceDocument, category string, now int64) (bool, error) {
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND status = ? AND operation_token = ?", document.ID, document.Status, document.OperationToken).
		Updates(map[string]any{
			"status": model.InvoiceDocumentStatusDeleteFailed, "delete_error_category": category,
			"last_delete_error": category, "next_delete_attempt_at": nil, "updated_at": now,
		})
	return updated.RowsAffected == 1, updated.Error
}

func claimInvoiceDocumentCleanupCandidate(db *gorm.DB, document model.InvoiceDocument, now int64) (string, bool, error) {
	token, err := generateInvoiceOperationToken()
	if err != nil {
		return "", false, err
	}
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND status = ? AND operation_token = ? AND delete_attempts = ?", document.ID, document.Status, document.OperationToken, document.DeleteAttempts).
		Updates(map[string]any{
			"status": model.InvoiceDocumentStatusDeleting, "operation_token": token,
			"operation_started_at": now, "delete_attempts": gorm.Expr("delete_attempts + ?", 1), "updated_at": now,
		})
	return token, updated.RowsAffected == 1, updated.Error
}

func finishInvoiceDocumentCleanupCandidate(db *gorm.DB, document model.InvoiceDocument, token string, deleteErr error, now int64) (bool, error) {
	if errors.Is(deleteErr, ErrInvoiceObjectNotFound) {
		deleteErr = nil
	}
	updates := map[string]any{"updated_at": now, "next_delete_attempt_at": nil}
	if deleteErr == nil {
		updates["status"] = model.InvoiceDocumentStatusDeleted
		updates["last_delete_error"] = ""
		updates["delete_error_category"] = nil
		updates["deleted_at"] = now
	} else {
		category, nextAt := classifyInvoiceDocumentDeleteFailure(deleteErr, document.DeleteAttempts+1, now)
		updates["status"] = model.InvoiceDocumentStatusDeleteFailed
		updates["delete_error_category"] = category
		updates["last_delete_error"] = category
		if nextAt != nil {
			updates["next_delete_attempt_at"] = *nextAt
		}
	}
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND status = ? AND operation_token = ?", document.ID, model.InvoiceDocumentStatusDeleting, token).
		Updates(updates)
	if updated.Error != nil {
		return false, updated.Error
	}
	if updated.RowsAffected != 1 {
		return false, model.ErrInvoiceDocumentConflict
	}
	return deleteErr == nil, nil
}

func classifyInvoiceDocumentDeleteFailure(deleteErr error, attempt int, now int64) (string, *int64) {
	if errors.Is(deleteErr, ErrInvoiceObjectBucketUnavailable) {
		return model.InvoiceDocumentDeleteErrorBucketMismatch, nil
	}
	if errors.Is(deleteErr, ErrInvoiceObjectRetryable) && attempt < invoiceCleanupMaxDeleteAttempts {
		if nextAt, ok := invoiceDocumentNextDeleteAttempt(now, attempt); ok {
			return model.InvoiceDocumentDeleteErrorRetryable, &nextAt
		}
	}
	return model.InvoiceDocumentDeleteErrorTerminal, nil
}

func invoiceDocumentNextDeleteAttempt(now int64, attempt int) (int64, bool) {
	if now <= 0 || attempt <= 0 || attempt >= invoiceCleanupMaxDeleteAttempts {
		return 0, false
	}
	exponent := attempt - 1
	if exponent > 7 {
		exponent = 7
	}
	delay := int64(15*time.Minute/time.Second) << exponent
	if delay > int64(24*time.Hour/time.Second) {
		delay = int64(24 * time.Hour / time.Second)
	}
	if now > math.MaxInt64-delay {
		return 0, false
	}
	return now + delay, true
}
