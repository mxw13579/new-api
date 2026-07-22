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

type InvoiceDocumentCleanupResult struct {
	Processed int `json:"processed"`
	Deleted   int `json:"deleted"`
	Failed    int `json:"failed"`
}

type invoiceDocumentCleanupHandler struct {
	db    *gorm.DB
	store InvoiceObjectStore
	now   func() int64
}

var _ ScheduledSystemTaskHandler = (*invoiceDocumentCleanupHandler)(nil)

func NewInvoiceDocumentCleanupHandler() SystemTaskHandler {
	return &invoiceDocumentCleanupHandler{db: model.DB, now: func() int64 { return time.Now().Unix() }}
}

func (*invoiceDocumentCleanupHandler) Type() string            { return model.InvoiceDocumentCleanupTaskType }
func (*invoiceDocumentCleanupHandler) Enabled() bool           { return true }
func (*invoiceDocumentCleanupHandler) Interval() time.Duration { return time.Hour }
func (*invoiceDocumentCleanupHandler) NewPayload() any         { return nil }

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
	result, err := CleanupInvoiceDocuments(ctx, handler.db, store, now, now-int64(invoiceCleanupLeaseAge.Seconds()), invoiceCleanupBatchSize)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, model.ErrSystemTaskLockLost) {
			return
		}
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, result, "invoice_cleanup_failed")
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, "")
}

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
