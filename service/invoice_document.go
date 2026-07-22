package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var (
	ErrInvoiceCommitAmbiguous   = errors.New("invoice document commit outcome is ambiguous")
	ErrInvoiceDocumentRetryable = errors.New("invoice document operation is retryable")
)

type InvoiceObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Copy(context.Context, string, string) error
	Head(context.Context, string) (InvoiceObjectHead, error)
	Delete(context.Context, string) error
	PresignGet(context.Context, string, time.Duration) (string, error)
}

type FinalizeInvoiceDocumentOperation struct {
	ApplicationID               int64
	DocumentID                  int64
	OperationToken              string
	ExpectedStatus              string
	ExpectedPaymentReviewStatus string
	ExpectedActiveDocumentID    *int64
	Issuance                    model.InvoiceIssuanceFacts
	PDFFactsAttested            bool
	AttestedBy                  int
	Now                         int64
}

type ReplaceInvoiceDocumentOperation struct {
	ApplicationID               int64
	NewDocumentID               int64
	OperationToken              string
	ExpectedStatus              string
	ExpectedPaymentReviewStatus string
	ExpectedActiveDocumentID    int64
	ExpectedIssuanceID          int64
	PDFFactsAttested            bool
	AttestedBy                  int
	Now                         int64
}

type InvoiceDocumentLifecycle struct {
	db             *gorm.DB
	store          InvoiceObjectStore
	application    model.InvoiceDocumentApplicationContract
	retentionDays  int
	runTransaction func(*gorm.DB, func(*gorm.DB) error) error
}

func NewInvoiceDocumentLifecycle(db *gorm.DB, store InvoiceObjectStore, application model.InvoiceDocumentApplicationContract, retentionDays int) *InvoiceDocumentLifecycle {
	lifecycle := &InvoiceDocumentLifecycle{
		db: db, store: store, application: application, retentionDays: retentionDays,
	}
	lifecycle.runTransaction = func(db *gorm.DB, operation func(*gorm.DB) error) error {
		return db.Transaction(operation)
	}
	return lifecycle
}

func CreateInvoiceDocumentUpload(db *gorm.DB, bucket string, applicationID int64, uploadedBy int, now int64) (*model.InvoiceDocument, error) {
	if db == nil || bucket == "" || applicationID <= 0 || uploadedBy <= 0 || now <= 0 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	stagingKey, err := generateInvoiceObjectKey("tmp/invoices/")
	if err != nil {
		return nil, err
	}
	operationToken, err := generateInvoiceOperationToken()
	if err != nil {
		return nil, err
	}
	document := &model.InvoiceDocument{
		ApplicationID: applicationID, R2Bucket: bucket, StagingObjectKey: &stagingKey,
		ContentType: model.InvoicePDFContentType, Status: model.InvoiceDocumentStatusUploading,
		OperationToken: operationToken, OperationStartedAt: now, UploadedBy: uploadedBy,
		UploadedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(document).Error; err != nil {
		return nil, err
	}
	return document, nil
}

func PromoteInvoiceDocument(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, documentID int64, operationToken string, reader io.Reader, now int64) (*model.InvoiceDocument, error) {
	if db == nil || store == nil || documentID <= 0 || operationToken == "" || reader == nil || now <= 0 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	var document model.InvoiceDocument
	if err := db.Where("id = ? AND operation_token = ? AND status = ?", documentID, operationToken, model.InvoiceDocumentStatusUploading).First(&document).Error; err != nil {
		return nil, invoiceDocumentLookupError(err)
	}
	if document.StagingObjectKey == nil {
		return nil, model.ErrInvoiceDocumentConflict
	}

	data, err := io.ReadAll(io.LimitReader(reader, InvoicePDFMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: upload read failed", ErrInvoiceDocumentRetryable)
	}
	validation, err := ValidateInvoicePDF(bytes.NewReader(data))
	if err != nil {
		if markErr := markInvoiceDocumentUploadFailed(db, document.ID, operationToken, now); markErr != nil {
			return nil, errors.Join(err, markErr)
		}
		return nil, err
	}
	checksum, err := invoiceObjectChecksum(validation.SHA256)
	if err != nil {
		return nil, err
	}
	if err := store.Put(ctx, *document.StagingObjectKey, bytes.NewReader(data), validation.SizeBytes, checksum); err != nil {
		return nil, err
	}

	finalKey, err := generateInvoiceObjectKey("invoices/")
	if err != nil {
		return nil, err
	}
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND operation_token = ? AND status = ?", document.ID, operationToken, model.InvoiceDocumentStatusUploading).
		Updates(map[string]any{
			"object_key": finalKey, "content_type": model.InvoicePDFContentType,
			"size_bytes": validation.SizeBytes, "sha256": validation.SHA256,
			"status": model.InvoiceDocumentStatusValidating, "operation_started_at": now, "updated_at": now,
		})
	if updated.Error != nil {
		return nil, updated.Error
	}
	if updated.RowsAffected != 1 {
		_ = deleteInvoiceObject(ctx, store, *document.StagingObjectKey)
		return nil, model.ErrInvoiceDocumentConflict
	}

	if err := store.Copy(ctx, *document.StagingObjectKey, finalKey); err != nil {
		return nil, err
	}
	head, err := store.Head(ctx, finalKey)
	if err != nil {
		return nil, err
	}
	if head.SizeBytes != validation.SizeBytes || head.ChecksumSHA256 != checksum {
		return nil, fmt.Errorf("%w: promoted object verification failed", ErrInvoiceDocumentRetryable)
	}
	_ = deleteInvoiceObject(ctx, store, *document.StagingObjectKey)

	if err := db.First(&document, document.ID).Error; err != nil {
		return nil, err
	}
	return &document, nil
}

func (lifecycle *InvoiceDocumentLifecycle) Finalize(ctx context.Context, operation FinalizeInvoiceDocumentOperation) (*model.InvoiceDocument, error) {
	if !validInvoiceAttestation(operation.PDFFactsAttested, operation.AttestedBy, operation.Now) || !validInvoiceIssuanceFacts(operation.Issuance) {
		return nil, model.ErrInvoiceDocumentConflict
	}
	document, err := lifecycle.validatingDocument(operation.DocumentID, operation.ApplicationID, operation.OperationToken)
	if err != nil {
		return nil, err
	}

	err = lifecycle.runTransaction(lifecycle.db, func(tx *gorm.DB) error {
		var current model.InvoiceDocument
		if err := tx.Where("id = ? AND application_id = ? AND operation_token = ? AND status = ?", operation.DocumentID, operation.ApplicationID, operation.OperationToken, model.InvoiceDocumentStatusValidating).First(&current).Error; err != nil {
			return invoiceDocumentLookupError(err)
		}
		prepared, err := lifecycle.application.PrepareDocumentTx(tx, model.PrepareInvoiceDocumentRequest{
			ApplicationID: operation.ApplicationID, ExpectedStatus: operation.ExpectedStatus,
			ExpectedPaymentReviewStatus: operation.ExpectedPaymentReviewStatus,
		})
		if err != nil {
			return err
		}
		if prepared.AmountMinor != operation.Issuance.FaceAmountMinor || prepared.Currency != operation.Issuance.Currency {
			return model.ErrInvoiceIssuanceConflict
		}
		finalized, err := lifecycle.application.FinalizeDocumentTx(tx, model.FinalizeInvoiceDocumentRequest{
			ApplicationID: operation.ApplicationID, DocumentID: operation.DocumentID,
			OperationToken: operation.OperationToken, ExpectedStatus: operation.ExpectedStatus,
			ExpectedPaymentReviewStatus: operation.ExpectedPaymentReviewStatus,
			ExpectedActiveDocumentID:    operation.ExpectedActiveDocumentID, Issuance: operation.Issuance,
			PDFFactsAttested: true, AttestedBy: operation.AttestedBy,
		})
		if err != nil {
			return err
		}
		if finalized.IssuanceID <= 0 || finalized.ActiveDocumentID != current.ID {
			return model.ErrInvoiceDocumentConflict
		}
		version := int64(1)
		return activateInvoiceDocumentTx(tx, &current, finalized.IssuanceID, version, operation.AttestedBy, operation.Now, prepared.ProfileSnapshot, lifecycle.retentionDays)
	})
	if err == nil {
		return lifecycle.documentByID(document.ID)
	}
	return lifecycle.resolveFinalizeFailure(ctx, document, operation.OperationToken, err)
}

func (lifecycle *InvoiceDocumentLifecycle) Replace(ctx context.Context, operation ReplaceInvoiceDocumentOperation) (*model.InvoiceDocument, error) {
	if !validInvoiceAttestation(operation.PDFFactsAttested, operation.AttestedBy, operation.Now) || operation.ExpectedActiveDocumentID <= 0 || operation.ExpectedIssuanceID <= 0 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	document, err := lifecycle.validatingDocument(operation.NewDocumentID, operation.ApplicationID, operation.OperationToken)
	if err != nil {
		return nil, err
	}

	err = lifecycle.runTransaction(lifecycle.db, func(tx *gorm.DB) error {
		var current, previous model.InvoiceDocument
		if err := tx.Where("id = ? AND application_id = ? AND operation_token = ? AND status = ?", operation.NewDocumentID, operation.ApplicationID, operation.OperationToken, model.InvoiceDocumentStatusValidating).First(&current).Error; err != nil {
			return invoiceDocumentLookupError(err)
		}
		if err := tx.Where("id = ? AND application_id = ? AND issuance_id = ? AND status = ?", operation.ExpectedActiveDocumentID, operation.ApplicationID, operation.ExpectedIssuanceID, model.InvoiceDocumentStatusAvailable).First(&previous).Error; err != nil {
			return invoiceDocumentLookupError(err)
		}
		if previous.Version == nil {
			return model.ErrInvoiceDocumentConflict
		}
		if *previous.Version == int64(^uint64(0)>>1) {
			return model.ErrInvoiceDocumentConflict
		}
		prepared, err := lifecycle.application.PrepareDocumentTx(tx, model.PrepareInvoiceDocumentRequest{
			ApplicationID: operation.ApplicationID, ExpectedStatus: operation.ExpectedStatus,
			ExpectedPaymentReviewStatus: operation.ExpectedPaymentReviewStatus,
		})
		if err != nil {
			return err
		}
		replaced, err := lifecycle.application.ReplaceDocumentTx(tx, model.ReplaceInvoiceDocumentRequest{
			ApplicationID: operation.ApplicationID, NewDocumentID: operation.NewDocumentID,
			OperationToken: operation.OperationToken, ExpectedStatus: operation.ExpectedStatus,
			ExpectedPaymentReviewStatus: operation.ExpectedPaymentReviewStatus,
			ExpectedActiveDocumentID:    operation.ExpectedActiveDocumentID, ExpectedIssuanceID: operation.ExpectedIssuanceID,
			PDFFactsAttested: true, AttestedBy: operation.AttestedBy,
		})
		if err != nil {
			return err
		}
		if replaced.SupersededDocumentID != previous.ID || replaced.ActiveDocumentID != current.ID {
			return model.ErrInvoiceDocumentConflict
		}
		superseded := tx.Model(&model.InvoiceDocument{}).
			Where("id = ? AND status = ?", previous.ID, model.InvoiceDocumentStatusAvailable).
			Updates(map[string]any{"status": model.InvoiceDocumentStatusSuperseded, "updated_at": operation.Now})
		if superseded.Error != nil {
			return superseded.Error
		}
		if superseded.RowsAffected != 1 {
			return model.ErrInvoiceDocumentConflict
		}
		return activateInvoiceDocumentTx(tx, &current, operation.ExpectedIssuanceID, *previous.Version+1, operation.AttestedBy, operation.Now, prepared.ProfileSnapshot, lifecycle.retentionDays)
	})
	if err == nil {
		return lifecycle.documentByID(document.ID)
	}
	return lifecycle.resolveFinalizeFailure(ctx, document, operation.OperationToken, err)
}

func ReconcileInvoiceDocument(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, documentID int64, now, staleBefore int64) (*model.InvoiceDocument, error) {
	if db == nil || store == nil || documentID <= 0 || now <= 0 || staleBefore <= 0 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	var document model.InvoiceDocument
	if err := db.Where("id = ? AND status IN ? AND operation_started_at <= ?", documentID,
		[]string{model.InvoiceDocumentStatusUploading, model.InvoiceDocumentStatusValidating}, staleBefore).First(&document).Error; err != nil {
		return nil, invoiceDocumentLookupError(err)
	}
	newToken, err := generateInvoiceOperationToken()
	if err != nil {
		return nil, err
	}
	claimed := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND status = ? AND operation_token = ? AND operation_started_at = ?", document.ID, document.Status, document.OperationToken, document.OperationStartedAt).
		Updates(map[string]any{"operation_token": newToken, "operation_started_at": now, "updated_at": now})
	if claimed.Error != nil {
		return nil, claimed.Error
	}
	if claimed.RowsAffected != 1 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	document.OperationToken = newToken
	document.OperationStartedAt = now

	key := document.StagingObjectKey
	if document.Status == model.InvoiceDocumentStatusValidating {
		key = document.ObjectKey
	}
	if key == nil {
		if err := markInvoiceDocumentUploadFailed(db, document.ID, newToken, now); err != nil {
			return nil, err
		}
		return lifecycleDocumentByID(db, document.ID)
	}
	head, err := store.Head(ctx, *key)
	if errors.Is(err, ErrInvoiceObjectNotFound) {
		if err := markInvoiceDocumentUploadFailed(db, document.ID, newToken, now); err != nil {
			return nil, err
		}
		return lifecycleDocumentByID(db, document.ID)
	}
	if err != nil {
		return nil, err
	}
	if document.Status == model.InvoiceDocumentStatusValidating {
		expectedChecksum, checksumErr := invoiceObjectChecksum(document.SHA256)
		if checksumErr != nil || head.SizeBytes != document.SizeBytes || head.ChecksumSHA256 != expectedChecksum {
			_ = deleteInvoiceObject(ctx, store, *key)
			if err := markInvoiceDocumentUploadFailed(db, document.ID, newToken, now); err != nil {
				return nil, err
			}
			return lifecycleDocumentByID(db, document.ID)
		}
	}
	return lifecycleDocumentByID(db, document.ID)
}

func activateInvoiceDocumentTx(tx *gorm.DB, document *model.InvoiceDocument, issuanceID, version int64, attestedBy int, now int64, profileSnapshot string, retentionDays int) error {
	if retentionDays <= 0 {
		return model.ErrInvoiceDocumentConflict
	}
	profileHash := sha256.Sum256([]byte(profileSnapshot))
	expiresAt := now + int64(retentionDays)*86400
	updated := tx.Model(&model.InvoiceDocument{}).
		Where("id = ? AND status = ? AND operation_token = ?", document.ID, model.InvoiceDocumentStatusValidating, document.OperationToken).
		Updates(map[string]any{
			"issuance_id": issuanceID, "version": version, "status": model.InvoiceDocumentStatusAvailable,
			"pdf_facts_attested": true, "attested_by": attestedBy, "attested_at": now,
			"attested_profile_snapshot_sha256": hex.EncodeToString(profileHash[:]),
			"available_at":                     now, "retention_days_snapshot": retentionDays, "expires_at": expiresAt, "updated_at": now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return model.ErrInvoiceDocumentConflict
	}
	return nil
}

func (lifecycle *InvoiceDocumentLifecycle) validatingDocument(documentID, applicationID int64, token string) (*model.InvoiceDocument, error) {
	if lifecycle == nil || lifecycle.db == nil || lifecycle.store == nil || lifecycle.application == nil || lifecycle.retentionDays <= 0 || documentID <= 0 || applicationID <= 0 || token == "" {
		return nil, model.ErrInvoiceDocumentConflict
	}
	var document model.InvoiceDocument
	if err := lifecycle.db.Where("id = ? AND application_id = ? AND operation_token = ? AND status = ?", documentID, applicationID, token, model.InvoiceDocumentStatusValidating).First(&document).Error; err != nil {
		return nil, invoiceDocumentLookupError(err)
	}
	if document.ObjectKey == nil || document.SHA256 == "" || document.SizeBytes <= 0 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	return &document, nil
}

func (lifecycle *InvoiceDocumentLifecycle) resolveFinalizeFailure(ctx context.Context, before *model.InvoiceDocument, token string, operationErr error) (*model.InvoiceDocument, error) {
	if errors.Is(operationErr, ErrInvoiceCommitAmbiguous) {
		current, err := lifecycle.documentByID(before.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: outcome reread failed", ErrInvoiceDocumentRetryable)
		}
		if current.OperationToken == token && current.Status == model.InvoiceDocumentStatusAvailable {
			return current, nil
		}
		if current.OperationToken != token || current.Status != model.InvoiceDocumentStatusValidating {
			return nil, fmt.Errorf("%w: outcome remains unproven", ErrInvoiceDocumentRetryable)
		}
	}
	if before.ObjectKey == nil {
		return nil, operationErr
	}
	if err := deleteInvoiceObject(ctx, lifecycle.store, *before.ObjectKey); err != nil {
		return nil, fmt.Errorf("%w: rollback cleanup failed", ErrInvoiceDocumentRetryable)
	}
	if markErr := markInvoiceDocumentUploadFailed(lifecycle.db, before.ID, token, before.OperationStartedAt); markErr != nil {
		return nil, errors.Join(operationErr, ErrInvoiceDocumentRetryable, markErr)
	}
	return nil, operationErr
}

func (lifecycle *InvoiceDocumentLifecycle) documentByID(id int64) (*model.InvoiceDocument, error) {
	return lifecycleDocumentByID(lifecycle.db, id)
}

func lifecycleDocumentByID(db *gorm.DB, id int64) (*model.InvoiceDocument, error) {
	var document model.InvoiceDocument
	if err := db.First(&document, id).Error; err != nil {
		return nil, invoiceDocumentLookupError(err)
	}
	return &document, nil
}

func markInvoiceDocumentUploadFailed(db *gorm.DB, id int64, token string, now int64) error {
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND operation_token = ? AND status IN ?", id, token, []string{model.InvoiceDocumentStatusUploading, model.InvoiceDocumentStatusValidating}).
		Updates(map[string]any{"status": model.InvoiceDocumentStatusUploadFailed, "updated_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return model.ErrInvoiceDocumentConflict
	}
	return nil
}

func deleteInvoiceObject(ctx context.Context, store InvoiceObjectStore, key string) error {
	err := store.Delete(ctx, key)
	if errors.Is(err, ErrInvoiceObjectNotFound) {
		return nil
	}
	return err
}

func invoiceDocumentLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ErrInvoiceDocumentConflict
	}
	return err
}

func validInvoiceAttestation(attested bool, actor int, now int64) bool {
	return attested && actor > 0 && now > 0
}

func validInvoiceIssuanceFacts(facts model.InvoiceIssuanceFacts) bool {
	return facts.InvoiceNumber != "" && facts.InvoiceDate > 0 && facts.FaceAmountMinor > 0 && facts.Currency == "CNY"
}

func generateInvoiceObjectKey(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random) + ".pdf", nil
}

func generateInvoiceOperationToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

func invoiceObjectChecksum(sha256Hex string) (string, error) {
	digest, err := hex.DecodeString(sha256Hex)
	if err != nil || len(digest) != sha256.Size {
		return "", model.ErrInvoiceDocumentConflict
	}
	return base64.StdEncoding.EncodeToString(digest), nil
}
