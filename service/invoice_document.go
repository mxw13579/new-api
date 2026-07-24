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
	"os"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var (
	// ErrInvoiceCommitAmbiguous indicates that document activation may have committed despite a lost commit response.
	ErrInvoiceCommitAmbiguous = errors.New("invoice document commit outcome is ambiguous")
	// ErrInvoiceDocumentRetryable indicates that durable state must be reread or reconciled before retrying the operation.
	ErrInvoiceDocumentRetryable = errors.New("invoice document operation is retryable")
	// ErrInvoiceStoreAuthorityUnbound indicates that a historical object has no explicitly attested store identity.
	ErrInvoiceStoreAuthorityUnbound = errors.New("invoice store authority is unbound")
	// ErrInvoiceStoreAuthorityMismatch indicates that persisted and configured store identities disagree.
	ErrInvoiceStoreAuthorityMismatch = errors.New("invoice store authority mismatch")
)

const (
	invoiceStoreAuthorityUnboundCategory  = "store_authority_unbound"
	invoiceStoreAuthorityMismatchCategory = "store_authority_mismatch"
)

// InvoiceObjectStore defines the private object operations required by invoice document lifecycle services.
type InvoiceObjectStore interface {
	AuthorityID() string
	Bucket() string
	Put(context.Context, string, io.Reader, int64, string) error
	Copy(context.Context, string, string) error
	Head(context.Context, string) (InvoiceObjectHead, error)
	Get(context.Context, string, string) (InvoiceObjectGet, error)
	Delete(context.Context, string) error
}

// FinalizeInvoiceDocumentOperation carries the exact CAS, issuance, and attestation facts for initial activation.
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

// ReplaceInvoiceDocumentOperation carries the exact active-document CAS and fresh attestation for replacement.
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

// InvoiceDocumentLifecycle coordinates durable document state with private object-store side effects.
type InvoiceDocumentLifecycle struct {
	db             *gorm.DB
	store          InvoiceObjectStore
	application    model.InvoiceDocumentApplicationContract
	retentionDays  int
	runTransaction func(*gorm.DB, func(*gorm.DB) error) error
}

// NewInvoiceDocumentLifecycle creates a document lifecycle bound to its database, object store, and retention policy.
func NewInvoiceDocumentLifecycle(db *gorm.DB, store InvoiceObjectStore, application model.InvoiceDocumentApplicationContract, retentionDays int) *InvoiceDocumentLifecycle {
	lifecycle := &InvoiceDocumentLifecycle{
		db: db, store: store, application: application, retentionDays: retentionDays,
	}
	lifecycle.runTransaction = func(db *gorm.DB, operation func(*gorm.DB) error) error {
		return runInvoiceDocumentTransaction(db, operation, nil)
	}
	return lifecycle
}

func runInvoiceDocumentTransaction(db *gorm.DB, operation func(*gorm.DB) error, commit func(*gorm.DB) error) (err error) {
	tx := db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	completed := false
	defer func() {
		if !completed {
			_ = tx.Rollback().Error
		}
		if recovered := recover(); recovered != nil {
			panic(recovered)
		}
	}()
	if err = operation(tx); err != nil {
		return err
	}
	if commit == nil {
		commit = func(tx *gorm.DB) error { return tx.Commit().Error }
	}
	err = commit(tx)
	completed = true
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvoiceCommitAmbiguous, err)
	}
	return nil
}

// CreateInvoiceDocumentUpload persists a new uploading lease with random non-PII staging and operation identifiers.
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

// PromoteInvoiceDocument validates and hashes PDF bytes before copying them from staging to a persisted final key.
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
	if err := bindNewInvoiceDocumentStore(db, store, &document, now); err != nil {
		return nil, err
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
			"operation_started_at": now, "updated_at": now,
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
	if head.SizeBytes != validation.SizeBytes || strings.TrimSpace(head.ETag) == "" ||
		(head.ChecksumSHA256 != "" && head.ChecksumSHA256 != checksum) {
		return nil, fmt.Errorf("%w: promoted object verification failed", ErrInvoiceDocumentRetryable)
	}
	if _, err := readVerifiedInvoiceObject(ctx, store, finalKey, head.ETag, validation.SizeBytes, validation.SHA256); err != nil {
		return nil, err
	}
	attested := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND operation_token = ? AND status = ? AND r2_authority_id = ? AND r2_bucket = ?", document.ID, operationToken,
			model.InvoiceDocumentStatusUploading, store.AuthorityID(), store.Bucket()).
		Updates(map[string]any{"object_etag": head.ETag, "updated_at": now})
	if attested.Error != nil {
		return nil, attested.Error
	}
	if attested.RowsAffected != 1 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	document.ObjectETag = &head.ETag
	if err := deleteInvoiceObject(ctx, store, *document.StagingObjectKey); err != nil {
		category := model.InvoiceDocumentRecoveryDeleteRetryable
		status := model.InvoiceDocumentStatusValidating
		if !errors.Is(err, ErrInvoiceObjectRetryable) {
			category = model.InvoiceDocumentRecoveryDeleteTerminal
			status = model.InvoiceDocumentStatusUploadFailed
		}
		if recordErr := recordInvoiceDocumentRecovery(db, document.ID, operationToken, status, category, now); recordErr != nil {
			return nil, errors.Join(err, recordErr)
		}
		return nil, err
	}
	ready := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND operation_token = ? AND status = ?", document.ID, operationToken, model.InvoiceDocumentStatusUploading).
		Updates(map[string]any{
			"status": model.InvoiceDocumentStatusValidating, "last_recovery_error": "", "updated_at": now,
		})
	if ready.Error != nil {
		return nil, ready.Error
	}
	if ready.RowsAffected != 1 {
		return nil, model.ErrInvoiceDocumentConflict
	}

	if err := db.First(&document, document.ID).Error; err != nil {
		return nil, err
	}
	return &document, nil
}

// Finalize atomically creates immutable issuance facts and activates the first attested document version.
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

// Replace atomically supersedes the active document and activates a newly attested version against exact CAS facts.
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
		if err := tx.Where("id = ? AND application_id = ? AND issuance_id = ? AND status IN ?", operation.ExpectedActiveDocumentID, operation.ApplicationID, operation.ExpectedIssuanceID,
			[]string{model.InvoiceDocumentStatusAvailable, model.InvoiceDocumentStatusMissing}).First(&previous).Error; err != nil {
			return invoiceDocumentLookupError(err)
		}
		if previous.Version == nil || previous.ExpiresAt == nil || *previous.ExpiresAt <= operation.Now {
			return model.ErrInvoiceDocumentConflict
		}
		oldStatus := model.InvoiceDocumentStatusSuperseded
		if previous.Status == model.InvoiceDocumentStatusMissing {
			if previous.DeleteErrorCategory == nil {
				oldStatus = model.InvoiceDocumentStatusMissing
			} else if *previous.DeleteErrorCategory != model.InvoiceDocumentDeleteErrorObjectIntegrityMismatch {
				return model.ErrInvoiceDocumentConflict
			}
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
		if oldStatus == model.InvoiceDocumentStatusSuperseded {
			oldDocument := tx.Model(&model.InvoiceDocument{}).
				Where("id = ? AND status = ? AND operation_token = ?", previous.ID, previous.Status, previous.OperationToken).
				Updates(map[string]any{"status": oldStatus, "updated_at": operation.Now})
			if oldDocument.Error != nil {
				return oldDocument.Error
			}
			if oldDocument.RowsAffected != 1 {
				return model.ErrInvoiceDocumentConflict
			}
		}
		return activateInvoiceDocumentTx(tx, &current, operation.ExpectedIssuanceID, *previous.Version+1, operation.AttestedBy, operation.Now, prepared.ProfileSnapshot, lifecycle.retentionDays)
	})
	if err == nil {
		return lifecycle.documentByID(document.ID)
	}
	return lifecycle.resolveFinalizeFailure(ctx, document, operation.OperationToken, err)
}

// ReconcileInvoiceDocument claims an expired upload lease and deterministically converges its persisted object state.
func ReconcileInvoiceDocument(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, documentID int64, now, staleBefore int64) (*model.InvoiceDocument, error) {
	if db == nil || store == nil || documentID <= 0 || now <= 0 || staleBefore <= 0 {
		return nil, model.ErrInvoiceDocumentConflict
	}
	var document model.InvoiceDocument
	if err := db.Where("id = ? AND ((status IN ? AND operation_started_at <= ?) OR (status = ? AND last_recovery_error = ? AND last_recovery_at <= ?))", documentID,
		[]string{model.InvoiceDocumentStatusUploading, model.InvoiceDocumentStatusValidating}, staleBefore,
		model.InvoiceDocumentStatusUploadFailed, model.InvoiceDocumentRecoveryDeleteRetryable, staleBefore).First(&document).Error; err != nil {
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
	if err := bindInvoiceDocumentStoreAuthority(ctx, db, store, &document); err != nil {
		return finishInvoiceDocumentRecovery(db, document, newToken, model.InvoiceDocumentStatusUploadFailed, invoiceStoreAuthorityCategory(err), now)
	}

	var application model.InvoiceApplication
	applicationErr := db.Select("id", "status", "active_document_id").First(&application, document.ApplicationID).Error
	if applicationErr != nil && !errors.Is(applicationErr, gorm.ErrRecordNotFound) {
		return nil, applicationErr
	}
	active := applicationErr == nil && application.ActiveDocumentID != nil && *application.ActiveDocumentID == document.ID
	if active {
		if !hasDurableInvoiceActivationFacts(document, application.Status, now) {
			return finishInvoiceDocumentRecovery(db, document, newToken, model.InvoiceDocumentStatusUploadFailed, model.InvoiceDocumentRecoveryActivationIncomplete, now)
		}
		if err := deletePersistedInvoiceObject(ctx, store, document.StagingObjectKey); err != nil {
			return finishInvoiceDocumentDeleteFailure(db, document, newToken, err, now)
		}
		return finishInvoiceDocumentRecovery(db, document, newToken, model.InvoiceDocumentStatusAvailable, "", now)
	}

	for _, key := range []*string{document.ObjectKey, document.StagingObjectKey} {
		if err := deletePersistedInvoiceObject(ctx, store, key); err != nil {
			return finishInvoiceDocumentDeleteFailure(db, document, newToken, err, now)
		}
	}
	return finishInvoiceDocumentRecovery(db, document, newToken, model.InvoiceDocumentStatusUploadFailed, "", now)
}

func finishInvoiceDocumentDeleteFailure(db *gorm.DB, document model.InvoiceDocument, token string, objectErr error, now int64) (*model.InvoiceDocument, error) {
	category := model.InvoiceDocumentRecoveryDeleteRetryable
	status := document.Status
	if !errors.Is(objectErr, ErrInvoiceObjectRetryable) {
		category = model.InvoiceDocumentRecoveryDeleteTerminal
		status = model.InvoiceDocumentStatusUploadFailed
	}
	return finishInvoiceDocumentRecovery(db, document, token, status, category, now)
}

func finishInvoiceDocumentRecovery(db *gorm.DB, document model.InvoiceDocument, token, status, category string, now int64) (*model.InvoiceDocument, error) {
	if err := recordInvoiceDocumentRecovery(db, document.ID, token, status, category, now); err != nil {
		current, readErr := lifecycleDocumentByID(db, document.ID)
		if readErr == nil {
			var application model.InvoiceApplication
			if appErr := db.Select("id", "status", "active_document_id").First(&application, document.ApplicationID).Error; appErr == nil &&
				application.ActiveDocumentID != nil && *application.ActiveDocumentID == current.ID && hasDurableInvoiceActivationFacts(*current, application.Status, now) {
				return current, nil
			}
		}
		return nil, err
	}
	return lifecycleDocumentByID(db, document.ID)
}

func recordInvoiceDocumentRecovery(db *gorm.DB, id int64, token, status, category string, now int64) error {
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND operation_token = ? AND status IN ?", id, token, []string{
			model.InvoiceDocumentStatusUploading, model.InvoiceDocumentStatusValidating, model.InvoiceDocumentStatusUploadFailed,
		}).Updates(map[string]any{
		"status": status, "recovery_attempts": gorm.Expr("recovery_attempts + ?", 1),
		"last_recovery_at": now, "last_recovery_error": category, "updated_at": now,
	})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return model.ErrInvoiceDocumentConflict
	}
	return nil
}

func deletePersistedInvoiceObject(ctx context.Context, store InvoiceObjectStore, key *string) error {
	if key == nil {
		return nil
	}
	return deleteInvoiceObject(ctx, store, *key)
}

func hasDurableInvoiceActivationFacts(document model.InvoiceDocument, applicationStatus string, now int64) bool {
	return applicationStatus == constant.InvoiceApplicationStatusIssued && document.IssuanceID != nil && *document.IssuanceID > 0 &&
		document.R2AuthorityID != nil && validInvoiceR2AuthorityID(*document.R2AuthorityID) && strings.TrimSpace(document.R2Bucket) != "" &&
		document.ObjectETag != nil && strings.TrimSpace(*document.ObjectETag) != "" &&
		document.Version != nil && *document.Version > 0 && document.PDFFactsAttested && document.AttestedBy != nil && *document.AttestedBy > 0 &&
		document.AttestedAt != nil && *document.AttestedAt > 0 && len(document.AttestedProfileSnapshotSHA256) == sha256.Size*2 &&
		document.AvailableAt != nil && *document.AvailableAt > 0 && document.RetentionDaysSnapshot > 0 && document.ExpiresAt != nil && *document.ExpiresAt > now
}

func activateInvoiceDocumentTx(tx *gorm.DB, document *model.InvoiceDocument, issuanceID, version int64, attestedBy int, now int64, profileSnapshot string, retentionDays int) error {
	expiresAt, err := invoiceDocumentExpiry(now, retentionDays)
	if err != nil {
		return model.ErrInvoiceDocumentConflict
	}
	profileHash := sha256.Sum256([]byte(profileSnapshot))
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

func invoiceDocumentExpiry(now int64, retentionDays int) (int64, error) {
	if now <= 0 || retentionDays <= 0 {
		return 0, model.ErrInvoiceDocumentConflict
	}
	days := int64(retentionDays)
	if days <= 0 || days > (int64(^uint64(0)>>1)-now)/86400 {
		return 0, model.ErrInvoiceDocumentConflict
	}
	return now + days*86400, nil
}

func (lifecycle *InvoiceDocumentLifecycle) validatingDocument(documentID, applicationID int64, token string) (*model.InvoiceDocument, error) {
	if lifecycle == nil || lifecycle.db == nil || lifecycle.store == nil || lifecycle.application == nil || lifecycle.retentionDays <= 0 || documentID <= 0 || applicationID <= 0 || token == "" {
		return nil, model.ErrInvoiceDocumentConflict
	}
	var document model.InvoiceDocument
	if err := lifecycle.db.Where("id = ? AND application_id = ? AND operation_token = ? AND status = ?", documentID, applicationID, token, model.InvoiceDocumentStatusValidating).First(&document).Error; err != nil {
		return nil, invoiceDocumentLookupError(err)
	}
	if document.ObjectKey == nil || document.SHA256 == "" || document.SizeBytes <= 0 || document.LastRecoveryError != "" ||
		document.R2AuthorityID == nil || *document.R2AuthorityID != lifecycle.store.AuthorityID() || document.R2Bucket != lifecycle.store.Bucket() ||
		document.ObjectETag == nil || strings.TrimSpace(*document.ObjectETag) == "" {
		return nil, model.ErrInvoiceDocumentConflict
	}
	return &document, nil
}

func bindNewInvoiceDocumentStore(db *gorm.DB, store InvoiceObjectStore, document *model.InvoiceDocument, now int64) error {
	if db == nil || store == nil || document == nil || !validInvoiceR2AuthorityID(store.AuthorityID()) ||
		strings.TrimSpace(store.Bucket()) == "" || document.R2Bucket != store.Bucket() {
		return ErrInvoiceStoreAuthorityMismatch
	}
	if document.R2AuthorityID != nil {
		if *document.R2AuthorityID != store.AuthorityID() {
			return ErrInvoiceStoreAuthorityMismatch
		}
		return nil
	}
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND r2_authority_id IS NULL AND r2_bucket = ? AND status = ? AND operation_token = ?", document.ID,
			document.R2Bucket, model.InvoiceDocumentStatusUploading, document.OperationToken).
		Updates(map[string]any{"r2_authority_id": store.AuthorityID(), "updated_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return model.ErrInvoiceDocumentConflict
	}
	authority := store.AuthorityID()
	document.R2AuthorityID = &authority
	return nil
}

func bindInvoiceDocumentStoreAuthority(ctx context.Context, db *gorm.DB, store InvoiceObjectStore, document *model.InvoiceDocument) error {
	if db == nil || store == nil || document == nil || !validInvoiceR2AuthorityID(store.AuthorityID()) ||
		strings.TrimSpace(store.Bucket()) == "" || document.R2Bucket != store.Bucket() {
		return ErrInvoiceStoreAuthorityMismatch
	}
	if document.R2AuthorityID != nil {
		if *document.R2AuthorityID != store.AuthorityID() {
			return ErrInvoiceStoreAuthorityMismatch
		}
		return nil
	}
	if os.Getenv("INVOICE_R2_LEGACY_AUTHORITY_ID") != store.AuthorityID() || document.ObjectKey == nil ||
		document.SizeBytes <= 0 || len(document.SHA256) != sha256.Size*2 || strings.TrimSpace(document.OperationToken) == "" {
		return ErrInvoiceStoreAuthorityUnbound
	}
	expectedChecksum, err := invoiceObjectChecksum(document.SHA256)
	if err != nil {
		return ErrInvoiceStoreAuthorityUnbound
	}
	head, err := store.Head(ctx, *document.ObjectKey)
	if err != nil || head.SizeBytes != document.SizeBytes || strings.TrimSpace(head.ETag) == "" ||
		(head.ChecksumSHA256 != "" && head.ChecksumSHA256 != expectedChecksum) {
		return ErrInvoiceStoreAuthorityUnbound
	}
	if _, err := readVerifiedInvoiceObject(ctx, store, *document.ObjectKey, head.ETag, document.SizeBytes, document.SHA256); err != nil {
		return ErrInvoiceStoreAuthorityUnbound
	}
	updated := db.Model(&model.InvoiceDocument{}).
		Where("id = ? AND r2_authority_id IS NULL AND r2_bucket = ? AND object_key = ? AND size_bytes = ? AND sha256 = ? AND status = ? AND operation_token = ?",
			document.ID, document.R2Bucket, *document.ObjectKey, document.SizeBytes, document.SHA256, document.Status, document.OperationToken).
		Updates(map[string]any{"r2_authority_id": store.AuthorityID(), "object_etag": head.ETag})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 1 {
		authority, etag := store.AuthorityID(), head.ETag
		document.R2AuthorityID, document.ObjectETag = &authority, &etag
		return nil
	}
	var current model.InvoiceDocument
	if err := db.First(&current, document.ID).Error; err != nil {
		return err
	}
	if current.R2AuthorityID != nil && *current.R2AuthorityID == store.AuthorityID() && current.R2Bucket == store.Bucket() &&
		current.ObjectETag != nil && *current.ObjectETag == head.ETag {
		*document = current
		return nil
	}
	if current.R2AuthorityID != nil {
		return ErrInvoiceStoreAuthorityMismatch
	}
	return ErrInvoiceStoreAuthorityUnbound
}

func invoiceStoreAuthorityCategory(err error) string {
	if errors.Is(err, ErrInvoiceStoreAuthorityMismatch) {
		return invoiceStoreAuthorityMismatchCategory
	}
	return invoiceStoreAuthorityUnboundCategory
}

func readVerifiedInvoiceObject(ctx context.Context, store InvoiceObjectStore, key, etag string, expectedSize int64, expectedSHA256 string) ([]byte, error) {
	if store == nil || expectedSize <= 0 || expectedSize > InvoicePDFMaxBytes || strings.TrimSpace(etag) == "" {
		return nil, ErrInvoiceObjectIntegrityUnavailable
	}
	expectedChecksum, err := invoiceObjectChecksum(expectedSHA256)
	if err != nil {
		return nil, ErrInvoiceObjectIntegrityUnavailable
	}
	object, err := store.Get(ctx, key, etag)
	if err != nil {
		return nil, err
	}
	if object.Body == nil {
		return nil, ErrInvoiceObjectIntegrityUnavailable
	}
	content, readErr := io.ReadAll(io.LimitReader(object.Body, InvoicePDFMaxBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("%w: object read interrupted", ErrInvoiceDocumentRetryable)
	}
	if int64(len(content)) > InvoicePDFMaxBytes || int64(len(content)) != expectedSize || object.SizeBytes != expectedSize ||
		object.ETag != etag || (object.ChecksumSHA256 != "" && object.ChecksumSHA256 != expectedChecksum) {
		return nil, ErrInvoiceObjectIntegrityUnavailable
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		return nil, ErrInvoiceObjectIntegrityUnavailable
	}
	if closeErr != nil {
		logger.LogWarn(ctx, "invoice document body close failed after verified read")
	}
	return content, nil
}

func (lifecycle *InvoiceDocumentLifecycle) resolveFinalizeFailure(ctx context.Context, before *model.InvoiceDocument, token string, operationErr error) (*model.InvoiceDocument, error) {
	if errors.Is(operationErr, ErrInvoiceCommitAmbiguous) {
		current, err := lifecycle.documentByID(before.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: outcome reread failed", ErrInvoiceDocumentRetryable)
		}
		var application model.InvoiceApplication
		if err := lifecycle.db.Select("id", "status", "active_document_id").First(&application, before.ApplicationID).Error; err != nil {
			return nil, fmt.Errorf("%w: application outcome reread failed", ErrInvoiceDocumentRetryable)
		}
		activated := current.OperationToken == token && current.Status == model.InvoiceDocumentStatusAvailable &&
			application.Status == constant.InvoiceApplicationStatusIssued && application.ActiveDocumentID != nil && *application.ActiveDocumentID == current.ID
		if activated {
			return current, nil
		}
		notActivated := current.OperationToken == token && current.Status == model.InvoiceDocumentStatusValidating &&
			(application.ActiveDocumentID == nil || *application.ActiveDocumentID != current.ID)
		if !notActivated {
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
