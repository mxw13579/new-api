// Copyright (C) 2023-2026 QuantumNous
// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "unsafe"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const (
	liveAuthority    = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	liveBucket       = "invoice-live-private"
	ownerToken       = "invoice-live-owner-token-00000001"
	mobileOwnerToken = "invoice-live-mobile-token-00000005"
	otherToken       = "invoice-live-other-token-00000002"
	reviewerToken    = "invoice-live-review-token-0000003"
	restrictedToken  = "invoice-live-denied-token-0000004"
)

var livePDF = buildLivePDF()

//go:linkname newInvoiceDownloadStore github.com/QuantumNous/new-api/service.newInvoiceDownloadStore
var newInvoiceDownloadStore func() (service.InvoiceObjectStore, error)

//go:linkname newInvoiceUploadStore github.com/QuantumNous/new-api/service.newInvoiceUploadStore
var newInvoiceUploadStore func(operation_setting.InvoiceSetting) (service.InvoiceObjectStore, error)

type liveObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (store *liveObjectStore) putRaw(key string, value []byte) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.objects[key] = append([]byte(nil), value...)
}

func (*liveObjectStore) AuthorityID() string { return liveAuthority }
func (*liveObjectStore) Bucket() string      { return liveBucket }
func (store *liveObjectStore) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	value, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.objects[key] = value
	return nil
}
func (store *liveObjectStore) Copy(_ context.Context, sourceKey, destinationKey string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.objects[sourceKey]
	if !ok {
		return service.ErrInvoiceObjectNotFound
	}
	store.objects[destinationKey] = append([]byte(nil), value...)
	return nil
}
func (store *liveObjectStore) Head(_ context.Context, key string) (service.InvoiceObjectHead, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.objects[key]
	if !ok {
		return service.InvoiceObjectHead{}, service.ErrInvoiceObjectNotFound
	}
	digest := sha256.Sum256(value)
	return service.InvoiceObjectHead{SizeBytes: int64(len(value)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), ETag: etag(value)}, nil
}
func (store *liveObjectStore) Get(_ context.Context, key, ifMatch string) (service.InvoiceObjectGet, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.objects[key]
	if !ok {
		return service.InvoiceObjectGet{}, service.ErrInvoiceObjectNotFound
	}
	if ifMatch != etag(value) {
		return service.InvoiceObjectGet{}, service.ErrInvoiceObjectIntegrityUnavailable
	}
	digest := sha256.Sum256(value)
	return service.InvoiceObjectGet{Body: io.NopCloser(bytes.NewReader(value)), SizeBytes: int64(len(value)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), ETag: etag(value)}, nil
}
func (store *liveObjectStore) Delete(_ context.Context, key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.objects[key]; !ok {
		return service.ErrInvoiceObjectNotFound
	}
	delete(store.objects, key)
	return nil
}

func etag(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("\"%x\"", digest[:16])
}

func main() {
	databasePath := filepath.Join(os.TempDir(), fmt.Sprintf("new-api-invoice-live-%d.db", os.Getpid()))
	defer os.Remove(databasePath)
	os.Setenv("SQLITE_PATH", databasePath+"?_busy_timeout=30000")
	os.Setenv("SESSION_SECRET", "invoice-live-test-session-secret-never-production")
	os.Setenv("MEMORY_CACHE_ENABLED", "false")
	os.Setenv("REDIS_CONN_STRING", "")
	common.InitEnv()
	common.RedisEnabled = false
	if err := model.InitDB(); err != nil {
		panic(err)
	}
	defer model.CloseDB()
	model.LOG_DB = model.DB
	if err := authz.Init(model.DB); err != nil {
		panic(err)
	}
	store := &liveObjectStore{objects: map[string][]byte{"invoices/live/private-object.pdf": livePDF}}
	seedLiveFixture()
	newInvoiceDownloadStore = func() (service.InvoiceObjectStore, error) { return store, nil }
	newInvoiceUploadStore = func(operation_setting.InvoiceSetting) (service.InvoiceObjectStore, error) { return store, nil }
	common.QuotaPerUnit = 0.81
	operation_setting.PublishInvoiceSetting(operation_setting.InvoiceSetting{PersonalEnabled: true, CompanyEnabled: true, ApplicationWindowDays: 30, MinimumAmountMinor: 1, FeePercent: 10, PDFRetentionDays: 30})

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(func(c *gin.Context) {
		if c.Request.URL.Path != "/api/user/auth/refresh" {
			c.Next()
			return
		}
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		username := ""
		switch token {
		case ownerToken:
			username = "invoice-live-owner"
		case mobileOwnerToken:
			username = "invoice-live-mobile"
		case reviewerToken:
			username = "invoice-live-reviewer"
		default:
			c.Next()
			return
		}
		var user model.User
		if err := model.DB.Where("username = ?", username).First(&user).Error; err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		now := time.Now().Unix()
		permissions := gin.H{"sidebar_settings": false}
		if token == reviewerToken {
			permissions["admin_permissions"] = gin.H{"invoice": gin.H{
				"review": true, "document.upload": true, "sensitive.read": false, "settings": false,
			}}
		}
		c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
			"access_token": token, "token_type": "Bearer", "access_expires_at": now + 3600,
			"user":    gin.H{"id": user.Id, "username": user.Username, "role": user.Role, "status": user.Status, "group": user.Group, "quota": user.Quota, "permissions": permissions},
			"session": gin.H{"sid": "invoice-live-browser", "current": true, "login_method": "live", "ip": "127.0.0.1", "user_agent": "playwright", "created_at": now - 60, "last_active_at": now, "expires_at": now + 3600},
		}})
	})
	router.SetApiRouter(engine)
	engine.GET("/__invoice-live/scenario/:name", func(c *gin.Context) {
		username := "invoice-live-owner"
		if c.Param("name") == "mobile" {
			username = "invoice-live-mobile"
		}
		var user model.User
		if err := model.DB.Where("username = ?", username).First(&user).Error; err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		var application model.InvoiceApplication
		if err := model.DB.Where("user_id = ? AND fee_charge_entry_id IS NOT NULL", user.Id).Order("id DESC").First(&application).Error; err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.JSON(http.StatusOK, gin.H{"application_id": application.ID, "application_no": application.ApplicationNo})
	})
	engine.GET("/__invoice-live/audit/:id", func(c *gin.Context) {
		applicationID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		counts := map[string]int64{}
		queries := []struct {
			name  string
			model any
			where string
			args  []any
		}{
			{"applications", &model.InvoiceApplication{}, "id = ?", []any{applicationID}},
			{"items", &model.InvoiceItem{}, "application_id = ?", []any{applicationID}},
			{"issuances", &model.InvoiceIssuance{}, "application_id = ?", []any{applicationID}},
			{"documents", &model.InvoiceDocument{}, "application_id = ?", []any{applicationID}},
			{"available_documents", &model.InvoiceDocument{}, "application_id = ? AND status = ?", []any{applicationID, model.InvoiceDocumentStatusAvailable}},
			{"superseded_documents", &model.InvoiceDocument{}, "application_id = ? AND status = ?", []any{applicationID, model.InvoiceDocumentStatusSuperseded}},
			{"deleted_documents", &model.InvoiceDocument{}, "application_id = ? AND status = ?", []any{applicationID, model.InvoiceDocumentStatusDeleted}},
			{"upload_failed_documents", &model.InvoiceDocument{}, "application_id = ? AND status = ?", []any{applicationID, model.InvoiceDocumentStatusUploadFailed}},
			{"fee_charges", &model.InvoiceFeeLedgerEntry{}, "application_id = ? AND entry_type = ?", []any{applicationID, model.InvoiceFeeEntryTypeCharge}},
		}
		for _, query := range queries {
			var count int64
			if err := model.DB.Model(query.model).Where(query.where, query.args...).Count(&count).Error; err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			counts[query.name] = count
		}
		var charge model.InvoiceFeeLedgerEntry
		if err := model.DB.Where("application_id = ? AND entry_type = ?", applicationID, model.InvoiceFeeEntryTypeCharge).First(&charge).Error; err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		result := gin.H{}
		for name, count := range counts {
			result[name] = count
		}
		result["fee_charge_quota"] = charge.Quota
		result["fee_charge_status"] = charge.Status
		result["fee_charge_balance_before"] = charge.BalanceBefore
		result["fee_charge_balance_after"] = charge.BalanceAfter
		c.JSON(http.StatusOK, result)
	})
	engine.POST("/__invoice-live/converge/:id", func(c *gin.Context) {
		applicationID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		now := time.Now().Unix()
		var application model.InvoiceApplication
		if err := model.DB.First(&application, applicationID).Error; err != nil || application.ActiveDocumentID == nil {
			c.Status(http.StatusNotFound)
			return
		}
		if err := model.DB.Model(&model.InvoiceDocument{}).Where("id = ?", *application.ActiveDocumentID).Update("expires_at", now-1).Error; err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		objectKey := fmt.Sprintf("invoices/live/recovery-%d.pdf", applicationID)
		stagingKey := fmt.Sprintf("tmp/invoices/live/recovery-%d.pdf", applicationID)
		store.putRaw(objectKey, livePDF)
		store.putRaw(stagingKey, livePDF)
		digest := sha256.Sum256(livePDF)
		recovery := model.InvoiceDocument{
			ApplicationID: applicationID, R2AuthorityID: stringPointer(liveAuthority), R2Bucket: liveBucket,
			ObjectKey: &objectKey, StagingObjectKey: &stagingKey, ContentType: model.InvoicePDFContentType,
			SizeBytes: int64(len(livePDF)), SHA256: fmt.Sprintf("%x", digest[:]), Status: model.InvoiceDocumentStatusValidating,
			OperationToken: strings.Repeat("c", 64), OperationStartedAt: now - 1000, UploadedBy: 1,
			UploadedAt: now - 1000, CreatedAt: now - 1000, UpdatedAt: now - 1000,
		}
		if err := model.DB.Create(&recovery).Error; err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		reconciled, err := service.ReconcileStaleInvoiceDocuments(c.Request.Context(), model.DB, store, now, now-500, 10)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		cleanup, err := service.CleanupInvoiceDocuments(c.Request.Context(), model.DB, store, now, now-500, 10)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.JSON(http.StatusOK, gin.H{"reconciled": reconciled, "cleanup_processed": cleanup.Processed, "cleanup_deleted": cleanup.Deleted, "cleanup_failed": cleanup.Failed})
	})
	engine.NoRoute(func(c *gin.Context) {
		path := filepath.Clean(filepath.Join("web", "dist", filepath.FromSlash(strings.TrimPrefix(c.Request.URL.Path, "/"))))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			c.File(path)
			return
		}
		c.File(filepath.Join("web", "dist", "index.html"))
	})
	port := os.Getenv("INVOICE_LIVE_PORT")
	if port == "" {
		port = "4187"
	}
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: engine, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func seedLiveFixture() {
	users := []model.User{
		{Username: "invoice-live-owner", Password: "unused", AccessToken: stringPointer(ownerToken), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-owner-aff", Quota: 100},
		{Username: "invoice-live-other", Password: "unused", AccessToken: stringPointer(otherToken), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-other-aff"},
		{Username: "invoice-live-reviewer", Password: "unused", AccessToken: stringPointer(reviewerToken), Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-review-aff"},
		{Username: "invoice-live-restricted", Password: "unused", AccessToken: stringPointer(restrictedToken), Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-denied-aff"},
		{Username: "invoice-live-mobile", Password: "unused", AccessToken: stringPointer(mobileOwnerToken), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-mobile-aff", Quota: 100},
		{Username: "invoice-live-root", Password: "unused", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-root-aff"},
	}
	for index := range users {
		if err := model.DB.Create(&users[index]).Error; err != nil {
			panic(err)
		}
	}
	seedChainFixture(users[0].Id, 7001, "desktop")
	seedChainFixture(users[4].Id, 7002, "mobile")
	if err := authz.SetUserPermissions(users[3].Id, authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: false}}); err != nil {
		panic(err)
	}
	now := nowUnix()
	application := model.InvoiceApplication{
		ApplicationNo: "INV-LIVE-0001", UserID: users[0].Id, RequestID: "invoice-live-request", RequestFingerprint: strings.Repeat("a", 64),
		Type: constant.InvoiceTypeCompany, Status: constant.InvoiceApplicationStatusIssued, PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
		Currency: constant.InvoiceCurrencyCNY, AmountMinor: 12345, FeeStatus: constant.InvoiceFeeStatusNotRequired,
		ProfileSnapshot: `{"type":"company","title":"Live Fixture Co","tax_number":"91310000PRIVATE","version":1}`,
		PolicySnapshot:  `{"application_window_days":30,"minimum_amount_minor":1,"fee_quota":0,"pdf_retention_days":30}`,
		SubmittedAt:     now - 60, IssuedAt: int64Pointer(now - 30),
	}
	if err := model.DB.Create(&application).Error; err != nil {
		panic(err)
	}
	digest := sha256.Sum256(livePDF)
	key := "invoices/live/private-object.pdf"
	document := model.InvoiceDocument{
		ApplicationID: application.ID, R2AuthorityID: stringPointer(liveAuthority), R2Bucket: liveBucket, ObjectKey: &key, ObjectETag: stringPointer(etag(livePDF)),
		ContentType: model.InvoicePDFContentType, SizeBytes: int64(len(livePDF)), SHA256: fmt.Sprintf("%x", digest[:]), Status: model.InvoiceDocumentStatusAvailable,
		OperationToken: strings.Repeat("b", 64), UploadedBy: users[2].Id, UploadedAt: now - 30, PDFFactsAttested: true,
		RetentionDaysSnapshot: 30, ExpiresAt: int64Pointer(now + 3600), CreatedAt: now - 30, UpdatedAt: now - 30,
	}
	if err := model.DB.Create(&document).Error; err != nil {
		panic(err)
	}
	if err := model.DB.Model(&application).Update("active_document_id", document.ID).Error; err != nil {
		panic(err)
	}
}

func seedChainFixture(userID, topupID int, suffix string) {
	profile := model.InvoiceProfile{UserID: userID, Type: constant.InvoiceTypeCompany, Title: "Live Fixture Co", TaxNumber: "91310000PRIVATE", IsDefault: true, Version: 1, CreatedAt: nowUnix(), UpdatedAt: nowUnix()}
	if err := model.DB.Create(&profile).Error; err != nil {
		panic(err)
	}
	providerTradeNo := "invoice-live-provider-trade-" + suffix
	_, providerTradeKey, err := model.NormalizeEpayProviderTradeIdentity(providerTradeNo)
	if err != nil {
		panic(err)
	}
	amount, eligible, refunded, version := int64(12345), true, int64(0), int64(1)
	currency, state := constant.InvoicePaymentEvidenceCurrencyCNY, constant.InvoicePaymentStateSucceeded
	product, source := constant.InvoicePaymentEvidenceTopUpProduct, constant.InvoicePaymentEvidenceSourceTrustedCallback
	topup := model.TopUp{Id: topupID, UserId: userID, Amount: 10, Money: 123.45, TradeNo: "invoice-live-topup-" + suffix, PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay, CompleteTime: nowUnix(), Status: common.TopUpStatusSuccess, PaidAmountMinor: &amount, Currency: &currency, InvoiceEligible: &eligible, PaymentState: &state, RefundedAmountMinor: &refunded, PaymentVersion: &version, ProductSnapshot: &product, PaymentEvidenceSource: &source, PaymentProviderTradeNo: &providerTradeNo, PaymentProviderTradeKey: &providerTradeKey}
	if err := model.DB.Create(&topup).Error; err != nil {
		panic(err)
	}
}

func stringPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64    { return &value }
func nowUnix() int64                     { return time.Now().Unix() }

func buildLivePDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
	}
	var output strings.Builder
	output.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&output, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(output.String())
}
