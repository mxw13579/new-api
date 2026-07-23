package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	invoiceCleanupBatchSize = 100
	invoiceCleanupLeaseAge  = 15 * time.Minute
)

// InvoiceDocumentCleanupResult summarizes bounded reconciliation and object-deletion work for one scheduled run.
type InvoiceDocumentCleanupResult struct {
	Reconciled int `json:"reconciled"`
	Processed  int `json:"processed"`
	Deleted    int `json:"deleted"`
	Failed     int `json:"failed"`
}

type invoiceDocumentCleanupHandler struct {
	db    *gorm.DB
	store InvoiceObjectStore
	now   func() int64
}

var _ ScheduledSystemTaskHandler = (*invoiceDocumentCleanupHandler)(nil)

// NewInvoiceDocumentCleanupHandler creates the hourly handler that reconciles stale uploads and expires stored PDFs.
func NewInvoiceDocumentCleanupHandler() SystemTaskHandler {
	return &invoiceDocumentCleanupHandler{db: model.DB, now: func() int64 { return time.Now().Unix() }}
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
	store := handler.store
	if store == nil {
		configured, err := NewInvoiceR2StoreFromEnvironment()
		if err != nil {
			_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "object_store_unavailable")
			return
		}
		store = configured
	}
	now := handler.now()
	staleBefore := now - int64(invoiceCleanupLeaseAge.Seconds())
	reconciled, err := ReconcileStaleInvoiceDocuments(ctx, handler.db, store, now, staleBefore, invoiceCleanupBatchSize)
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
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, result, "invoice_cleanup_failed")
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, "")
}

// ReconcileStaleInvoiceDocuments converges a bounded batch of expired upload leases without guessing object keys.
func ReconcileStaleInvoiceDocuments(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, now, staleBefore int64, limit int) (int, error) {
	if db == nil || store == nil || now <= 0 || staleBefore <= 0 || limit <= 0 {
		return 0, model.ErrInvoiceDocumentConflict
	}
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
	var documents []model.InvoiceDocument
	err := db.Where(
		"(status = ? AND expires_at IS NOT NULL AND expires_at <= ?) OR status IN ? OR (status = ? AND operation_started_at <= ?)",
		model.InvoiceDocumentStatusAvailable, now,
		[]string{model.InvoiceDocumentStatusDeleteFailed, model.InvoiceDocumentStatusSuperseded},
		model.InvoiceDocumentStatusDeleting, staleBefore,
	).Order("id asc").Limit(limit).Find(&documents).Error
	if err != nil {
		return result, err
	}
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !invoiceObjectStoreMatchesBucket(store, document.R2Bucket) {
			return result, ErrInvoiceObjectTerminal
		}
		token, err := generateInvoiceOperationToken()
		if err != nil {
			return result, err
		}
		claimed := db.Model(&model.InvoiceDocument{}).
			Where("id = ? AND status = ? AND operation_token = ?", document.ID, document.Status, document.OperationToken).
			Updates(map[string]any{"status": model.InvoiceDocumentStatusDeleting, "operation_token": token, "operation_started_at": now, "updated_at": now})
		if claimed.Error != nil {
			return result, claimed.Error
		}
		if claimed.RowsAffected != 1 {
			continue
		}
		result.Processed++
		deleteErr := error(nil)
		if document.ObjectKey != nil {
			deleteErr = store.Delete(ctx, *document.ObjectKey)
		}
		if errors.Is(deleteErr, ErrInvoiceObjectNotFound) {
			deleteErr = nil
		}
		updates := map[string]any{"delete_attempts": gorm.Expr("delete_attempts + ?", 1), "updated_at": now}
		if deleteErr == nil {
			updates["status"] = model.InvoiceDocumentStatusDeleted
			updates["last_delete_error"] = ""
			updates["deleted_at"] = now
			result.Deleted++
		} else {
			updates["status"] = model.InvoiceDocumentStatusDeleteFailed
			updates["last_delete_error"] = "object_delete_failed"
			result.Failed++
		}
		finished := db.Model(&model.InvoiceDocument{}).
			Where("id = ? AND status = ? AND operation_token = ?", document.ID, model.InvoiceDocumentStatusDeleting, token).
			Updates(updates)
		if finished.Error != nil {
			return result, finished.Error
		}
		if finished.RowsAffected != 1 {
			return result, model.ErrInvoiceDocumentConflict
		}
	}
	return result, nil
}
