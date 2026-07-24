package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormPostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const invoiceR2ReviewDatabasePrefix = "newapi_invoice_r2_review_"

var invoiceR2ReviewDatabaseNamePattern = regexp.MustCompile(`^` + invoiceR2ReviewDatabasePrefix + `[a-z0-9_]{1,34}$`)

func TestInvoiceR2ReviewPostgreSQLRaces(t *testing.T) {
	db, enabled := openInvoiceR2ReviewPostgreSQL(t)
	if !enabled {
		t.Skip("set TEST_POSTGRES_DSN and TEST_INVOICE_POSTGRES_DATABASE to run invoice R2 review PostgreSQL races")
	}
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}, &model.InvoiceIssuance{}, &model.InvoiceDocument{}))

	t.Run("initial issuance has one winner", func(t *testing.T) {
		application := seedInvoiceR2ReviewApplication(t, db, "initial", constant.InvoiceApplicationStatusApproved)
		documents := seedInvoiceR2ReviewCandidates(t, db, application.ID, "initial", 2)
		operations := []FinalizeInvoiceDocumentOperation{
			finalizeInvoiceR2ReviewOperation(application, documents[0], "INV-R2-PG-INITIAL", 100),
			finalizeInvoiceR2ReviewOperation(application, documents[1], "INV-R2-PG-INITIAL", 101),
		}

		results := runInvoiceR2ReviewFinalizeRace(t, db, operations)
		winner := requireInvoiceR2ReviewSingleWinner(t, results, model.ErrInvoiceStateConflict)

		var persisted model.InvoiceApplication
		require.NoError(t, db.First(&persisted, application.ID).Error)
		assert.Equal(t, constant.InvoiceApplicationStatusIssued, persisted.Status)
		require.NotNil(t, persisted.ActiveDocumentID)
		assert.Equal(t, results[winner].document.ID, *persisted.ActiveDocumentID)
		assertInvoiceR2ReviewFinalState(t, db, application.ID, 1, 1, 0, 1)
	})

	t.Run("same active replacement has one winner", func(t *testing.T) {
		application, issuance, active := seedIssuedInvoiceR2ReviewApplication(t, db, "replacement")
		candidates := seedInvoiceR2ReviewCandidates(t, db, application.ID, "replacement", 2)
		operations := []ReplaceInvoiceDocumentOperation{
			replaceInvoiceR2ReviewOperation(application, issuance, active, candidates[0], 200),
			replaceInvoiceR2ReviewOperation(application, issuance, active, candidates[1], 201),
		}

		results := runInvoiceR2ReviewReplacementRace(t, db, operations)
		winner := requireInvoiceR2ReviewSingleWinner(t, results, model.ErrInvoiceStateConflict)

		var persisted model.InvoiceApplication
		require.NoError(t, db.First(&persisted, application.ID).Error)
		require.NotNil(t, persisted.ActiveDocumentID)
		assert.Equal(t, results[winner].document.ID, *persisted.ActiveDocumentID)
		assertInvoiceR2ReviewFinalState(t, db, application.ID, 1, 1, 1, 1)
	})

	t.Run("invoice number is globally unique across applications", func(t *testing.T) {
		applications := []*model.InvoiceApplication{
			seedInvoiceR2ReviewApplication(t, db, "number-a", constant.InvoiceApplicationStatusApproved),
			seedInvoiceR2ReviewApplication(t, db, "number-b", constant.InvoiceApplicationStatusApproved),
		}
		documents := []*model.InvoiceDocument{
			seedInvoiceR2ReviewCandidates(t, db, applications[0].ID, "number-a", 1)[0],
			seedInvoiceR2ReviewCandidates(t, db, applications[1].ID, "number-b", 1)[0],
		}
		operations := []FinalizeInvoiceDocumentOperation{
			finalizeInvoiceR2ReviewOperation(applications[0], documents[0], "INV-R2-PG-SHARED", 300),
			finalizeInvoiceR2ReviewOperation(applications[1], documents[1], "INV-R2-PG-SHARED", 301),
		}

		results := runInvoiceR2ReviewFinalizeRace(t, db, operations)
		requireInvoiceR2ReviewSingleWinner(t, results, model.ErrInvoiceIssuanceConflict)

		var issuances int64
		require.NoError(t, db.Model(&model.InvoiceIssuance{}).Where("invoice_number = ?", "INV-R2-PG-SHARED").Count(&issuances).Error)
		assert.Equal(t, int64(1), issuances)
		var issued, approved, available, failed int64
		require.NoError(t, db.Model(&model.InvoiceApplication{}).Where("id IN ? AND status = ?", []int64{applications[0].ID, applications[1].ID}, constant.InvoiceApplicationStatusIssued).Count(&issued).Error)
		require.NoError(t, db.Model(&model.InvoiceApplication{}).Where("id IN ? AND status = ? AND active_document_id IS NULL", []int64{applications[0].ID, applications[1].ID}, constant.InvoiceApplicationStatusApproved).Count(&approved).Error)
		require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("application_id IN ? AND status = ? AND version = ?", []int64{applications[0].ID, applications[1].ID}, model.InvoiceDocumentStatusAvailable, 1).Count(&available).Error)
		require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("application_id IN ? AND status = ? AND issuance_id IS NULL AND version IS NULL", []int64{applications[0].ID, applications[1].ID}, model.InvoiceDocumentStatusUploadFailed).Count(&failed).Error)
		assert.Equal(t, int64(1), issued)
		assert.Equal(t, int64(1), approved)
		assert.Equal(t, int64(1), available)
		assert.Equal(t, int64(1), failed)
	})
}

type invoiceR2ReviewRaceResult struct {
	document *model.InvoiceDocument
	err      error
}

func requireInvoiceR2ReviewSingleWinner(t *testing.T, results []invoiceR2ReviewRaceResult, conflict error) int {
	t.Helper()
	require.Len(t, results, 2)
	winner := -1
	for index, result := range results {
		if result.err == nil {
			require.NotNil(t, result.document)
			require.Equal(t, -1, winner, "race produced more than one winner")
			winner = index
			continue
		}
		assert.ErrorIs(t, result.err, conflict)
		assertInvoiceR2ReviewStableConflict(t, result.err)
	}
	require.NotEqual(t, -1, winner, "race produced no winner")
	return winner
}

func assertInvoiceR2ReviewStableConflict(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.True(t, errors.Is(err, model.ErrInvoiceStateConflict) || errors.Is(err, model.ErrInvoiceIssuanceConflict))
	message := strings.ToLower(err.Error())
	assert.NotContains(t, message, "sqlstate")
	assert.NotContains(t, message, "duplicate key")
	assert.NotContains(t, message, "constraint")
}

func openInvoiceR2ReviewPostgreSQL(t *testing.T) (*gorm.DB, bool) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	databaseName := os.Getenv("TEST_INVOICE_POSTGRES_DATABASE")
	if dsn == "" && databaseName == "" {
		return nil, false
	}
	require.NotEmpty(t, dsn, "TEST_POSTGRES_DSN is required")
	require.Regexp(t, invoiceR2ReviewDatabaseNamePattern, databaseName, "TEST_INVOICE_POSTGRES_DATABASE must be a unique invoice R2 review database")

	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = databaseName
	admin, err := gorm.Open(gormPostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal("PostgreSQL admin connection failed")
	}
	var existing int64
	require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", databaseName).Scan(&existing).Error)
	require.Zero(t, existing, "isolated PostgreSQL database already exists")
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+databaseName+`" TEMPLATE template0`).Error)

	testSQL := stdlib.OpenDB(*config)
	db, err := gorm.Open(gormPostgres.New(gormPostgres.Config{Conn: testSQL, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + databaseName + `"`).Error
		t.Fatal("isolated PostgreSQL connection failed")
	}
	var currentDatabase string
	if err := db.Raw("SELECT current_database()").Scan(&currentDatabase).Error; err != nil || currentDatabase != databaseName {
		_ = testSQL.Close()
		_ = admin.Exec(`DROP DATABASE "` + databaseName + `"`).Error
		t.Fatal("isolated PostgreSQL binding verification failed before writes")
	}

	previousMainType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	t.Cleanup(func() {
		common.SetMainDatabaseType(previousMainType)
		require.NoError(t, testSQL.Close())
		require.NoError(t, admin.Exec(`DROP DATABASE "`+databaseName+`"`).Error)
		var residual int64
		require.NoError(t, admin.Raw("SELECT COUNT(*) FROM pg_database WHERE LEFT(datname, LENGTH(?)) = ?", invoiceR2ReviewDatabasePrefix, invoiceR2ReviewDatabasePrefix).Scan(&residual).Error)
		require.Zero(t, residual, "invoice R2 review PostgreSQL database cleanup left a residual database")
		adminSQL, closeErr := admin.DB()
		if closeErr == nil {
			require.NoError(t, adminSQL.Close())
		}
	})
	return db, true
}

type invoiceR2ReviewProductionLockBarrier struct {
	oldLocked       chan string
	newBeforeQuery  chan struct{}
	releaseOld      chan struct{}
	applicationRead atomic.Int32
}

type invoiceR2ReviewObjectStore struct{}

func (invoiceR2ReviewObjectStore) AuthorityID() string { return invoiceTestAuthorityID }
func (invoiceR2ReviewObjectStore) Bucket() string      { return "invoice-r2-review-test" }

func (invoiceR2ReviewObjectStore) Put(context.Context, string, io.Reader, int64, string) error {
	return nil
}

func (invoiceR2ReviewObjectStore) Copy(context.Context, string, string) error { return nil }

func (invoiceR2ReviewObjectStore) Head(context.Context, string) (InvoiceObjectHead, error) {
	return InvoiceObjectHead{}, nil
}

func (invoiceR2ReviewObjectStore) Get(context.Context, string, string) (InvoiceObjectGet, error) {
	return InvoiceObjectGet{Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (invoiceR2ReviewObjectStore) Delete(context.Context, string) error { return nil }

func (invoiceR2ReviewObjectStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func runInvoiceR2ReviewFinalizeRace(t *testing.T, db *gorm.DB, operations []FinalizeInvoiceDocumentOperation) []invoiceR2ReviewRaceResult {
	t.Helper()
	require.Len(t, operations, 2)
	return runInvoiceR2ReviewProductionLockRace(t, db, fmt.Sprintf("finalize-%d", operations[0].DocumentID), func(index int) invoiceR2ReviewRaceResult {
		lifecycle := NewInvoiceDocumentLifecycle(db, invoiceR2ReviewObjectStore{}, model.NewInvoiceDocumentApplicationContract(), 30)
		document, err := lifecycle.Finalize(context.Background(), operations[index])
		return invoiceR2ReviewRaceResult{document: document, err: err}
	})
}

func runInvoiceR2ReviewReplacementRace(t *testing.T, db *gorm.DB, operations []ReplaceInvoiceDocumentOperation) []invoiceR2ReviewRaceResult {
	t.Helper()
	require.Len(t, operations, 2)
	return runInvoiceR2ReviewProductionLockRace(t, db, fmt.Sprintf("replace-%d", operations[0].NewDocumentID), func(index int) invoiceR2ReviewRaceResult {
		lifecycle := NewInvoiceDocumentLifecycle(db, invoiceR2ReviewObjectStore{}, model.NewInvoiceDocumentApplicationContract(), 30)
		document, err := lifecycle.Replace(context.Background(), operations[index])
		return invoiceR2ReviewRaceResult{document: document, err: err}
	})
}

func runInvoiceR2ReviewProductionLockRace(t *testing.T, db *gorm.DB, callbackSuffix string, operation func(int) invoiceR2ReviewRaceResult) []invoiceR2ReviewRaceResult {
	t.Helper()
	barrier := &invoiceR2ReviewProductionLockBarrier{
		oldLocked: make(chan string, 1), newBeforeQuery: make(chan struct{}), releaseOld: make(chan struct{}),
	}
	defer func() {
		select {
		case <-barrier.releaseOld:
		default:
			close(barrier.releaseOld)
		}
	}()
	marker := "invoice-r2-review-old-lock-" + callbackSuffix
	beforeCallback := "test:invoice-r2-review-before-lock-" + callbackSuffix
	afterCallback := "test:invoice-r2-review-after-lock-" + callbackSuffix
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(beforeCallback, func(tx *gorm.DB) {
		if tx.Statement.Table != "invoice_applications" {
			return
		}
		switch barrier.applicationRead.Add(1) {
		case 1:
			tx.InstanceSet(marker, true)
		case 2:
			close(barrier.newBeforeQuery)
		}
	}))
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(afterCallback, func(tx *gorm.DB) {
		if _, marked := tx.InstanceGet(marker); !marked {
			return
		}
		barrier.oldLocked <- tx.Statement.SQL.String()
		<-barrier.releaseOld
	}))
	defer func() {
		_ = db.Callback().Query().Remove(beforeCallback)
		_ = db.Callback().Query().Remove(afterCallback)
	}()

	results := make(chan invoiceR2ReviewRaceResult, 2)
	go func() { results <- operation(0) }()
	var lockedSQL string
	select {
	case lockedSQL = <-barrier.oldLocked:
	case <-time.After(10 * time.Second):
		t.Fatal("old PostgreSQL transaction did not complete its production locking read")
	}
	require.Contains(t, strings.ToUpper(lockedSQL), "FOR UPDATE")

	go func() { results <- operation(1) }()
	select {
	case <-barrier.newBeforeQuery:
	case <-time.After(10 * time.Second):
		t.Fatal("new PostgreSQL transaction did not enter the production locking read")
	}
	close(barrier.releaseOld)
	first, second := <-results, <-results
	require.GreaterOrEqual(t, barrier.applicationRead.Load(), int32(2), "production lock barrier callbacks were not both hit")
	return []invoiceR2ReviewRaceResult{first, second}
}

func seedInvoiceR2ReviewApplication(t *testing.T, db *gorm.DB, suffix, status string) *model.InvoiceApplication {
	t.Helper()
	application := &model.InvoiceApplication{
		ApplicationNo: "R2-PG-" + suffix, UserID: 9001, RequestID: "r2-pg-" + suffix,
		RequestFingerprint: strings.Repeat("f", 64), Type: constant.InvoiceTypePersonal,
		Status: status, PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		Currency: constant.InvoiceCurrencyCNY, AmountMinor: 500, FeeMethod: model.InvoiceFeeMethodWalletQuota,
		FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{"title":"PostgreSQL proof"}`,
		PolicySnapshot: `{}`, SubmittedAt: 1,
	}
	require.NoError(t, db.Create(application).Error)
	return application
}

func seedInvoiceR2ReviewCandidates(t *testing.T, db *gorm.DB, applicationID int64, suffix string, count int) []*model.InvoiceDocument {
	t.Helper()
	documents := make([]*model.InvoiceDocument, 0, count)
	for index := range count {
		objectKey := fmt.Sprintf("invoices/r2-pg-%s-%d.pdf", suffix, index)
		document := &model.InvoiceDocument{
			ApplicationID: applicationID, R2Bucket: "invoice-r2-review-test", ObjectKey: &objectKey,
			ContentType: model.InvoicePDFContentType, SizeBytes: 100, SHA256: strings.Repeat("a", 64),
			Status: model.InvoiceDocumentStatusValidating, OperationToken: fmt.Sprintf("r2-pg-%s-token-%d", suffix, index),
			OperationStartedAt: 10, UploadedBy: 700 + index, UploadedAt: 10, CreatedAt: 10, UpdatedAt: 10,
		}
		require.NoError(t, db.Create(document).Error)
		documents = append(documents, document)
	}
	return documents
}

func seedIssuedInvoiceR2ReviewApplication(t *testing.T, db *gorm.DB, suffix string) (*model.InvoiceApplication, *model.InvoiceIssuance, *model.InvoiceDocument) {
	t.Helper()
	application := seedInvoiceR2ReviewApplication(t, db, suffix, constant.InvoiceApplicationStatusIssued)
	issuance := &model.InvoiceIssuance{
		ApplicationID: application.ID, InvoiceNumber: "INV-R2-PG-EXISTING", InvoiceDate: 50,
		FaceAmountMinor: application.AmountMinor, Currency: application.Currency, CreatedBy: 600, CreatedAt: 50, UpdatedAt: 50,
	}
	require.NoError(t, db.Create(issuance).Error)
	objectKey := "invoices/r2-pg-existing.pdf"
	version := int64(1)
	availableAt, expiresAt, actor := int64(50), int64(5000000), 600
	active := &model.InvoiceDocument{
		ApplicationID: application.ID, IssuanceID: &issuance.ID, Version: &version,
		R2Bucket: "invoice-r2-review-test", ObjectKey: &objectKey, ContentType: model.InvoicePDFContentType,
		SizeBytes: 100, SHA256: strings.Repeat("b", 64), Status: model.InvoiceDocumentStatusAvailable,
		OperationToken: "r2-pg-existing-token", OperationStartedAt: 50, UploadedBy: actor, UploadedAt: 50,
		PDFFactsAttested: true, AttestedBy: &actor, AttestedAt: &availableAt,
		AttestedProfileSnapshotSHA256: strings.Repeat("c", 64), AvailableAt: &availableAt,
		RetentionDaysSnapshot: 30, ExpiresAt: &expiresAt, CreatedAt: 50, UpdatedAt: 50,
	}
	require.NoError(t, db.Create(active).Error)
	issuedAt := int64(50)
	require.NoError(t, db.Model(application).Updates(map[string]any{"active_document_id": active.ID, "issued_at": issuedAt}).Error)
	application.ActiveDocumentID = &active.ID
	application.IssuedAt = &issuedAt
	return application, issuance, active
}

func finalizeInvoiceR2ReviewOperation(application *model.InvoiceApplication, document *model.InvoiceDocument, invoiceNumber string, now int64) FinalizeInvoiceDocumentOperation {
	return FinalizeInvoiceDocumentOperation{
		ApplicationID: application.ID, DocumentID: document.ID, OperationToken: document.OperationToken,
		ExpectedStatus:              constant.InvoiceApplicationStatusApproved,
		ExpectedPaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		Issuance:                    model.InvoiceIssuanceFacts{InvoiceNumber: invoiceNumber, InvoiceDate: now, FaceAmountMinor: application.AmountMinor, Currency: application.Currency},
		PDFFactsAttested:            true, AttestedBy: 800, Now: now,
	}
}

func replaceInvoiceR2ReviewOperation(application *model.InvoiceApplication, issuance *model.InvoiceIssuance, active, candidate *model.InvoiceDocument, now int64) ReplaceInvoiceDocumentOperation {
	return ReplaceInvoiceDocumentOperation{
		ApplicationID: application.ID, NewDocumentID: candidate.ID, OperationToken: candidate.OperationToken,
		ExpectedStatus:              constant.InvoiceApplicationStatusIssued,
		ExpectedPaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		ExpectedActiveDocumentID:    active.ID, ExpectedIssuanceID: issuance.ID,
		PDFFactsAttested: true, AttestedBy: 801, Now: now,
	}
}

func assertInvoiceR2ReviewFinalState(t *testing.T, db *gorm.DB, applicationID, issuanceCount, availableCount, supersededCount, failedCount int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.InvoiceIssuance{}).Where("application_id = ?", applicationID).Count(&count).Error)
	assert.Equal(t, issuanceCount, count)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("application_id = ? AND status = ?", applicationID, model.InvoiceDocumentStatusAvailable).Count(&count).Error)
	assert.Equal(t, availableCount, count)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("application_id = ? AND status = ?", applicationID, model.InvoiceDocumentStatusSuperseded).Count(&count).Error)
	assert.Equal(t, supersededCount, count)
	require.NoError(t, db.Model(&model.InvoiceDocument{}).Where("application_id = ? AND status = ? AND issuance_id IS NULL AND version IS NULL", applicationID, model.InvoiceDocumentStatusUploadFailed).Count(&count).Error)
	assert.Equal(t, failedCount, count)

	var application model.InvoiceApplication
	require.NoError(t, db.First(&application, applicationID).Error)
	require.NotNil(t, application.ActiveDocumentID)
	var active model.InvoiceDocument
	require.NoError(t, db.First(&active, *application.ActiveDocumentID).Error)
	assert.Equal(t, model.InvoiceDocumentStatusAvailable, active.Status)
	require.NotNil(t, active.IssuanceID)
	require.NotNil(t, active.Version)
	expectedVersion := int64(1)
	if supersededCount == 1 {
		expectedVersion = 2
	}
	assert.Equal(t, expectedVersion, *active.Version)
	assert.True(t, active.PDFFactsAttested)
	require.NotNil(t, active.AttestedBy)
	require.NotNil(t, active.AvailableAt)
	require.NotNil(t, active.ExpiresAt)
	assert.Len(t, active.AttestedProfileSnapshotSHA256, 64)
	if supersededCount == 1 {
		var superseded model.InvoiceDocument
		require.NoError(t, db.Where("application_id = ? AND status = ?", applicationID, model.InvoiceDocumentStatusSuperseded).First(&superseded).Error)
		require.NotNil(t, superseded.IssuanceID)
		require.NotNil(t, superseded.Version)
		assert.Equal(t, *active.IssuanceID, *superseded.IssuanceID)
		assert.Equal(t, int64(1), *superseded.Version)
		assert.True(t, superseded.PDFFactsAttested)
	}
}
