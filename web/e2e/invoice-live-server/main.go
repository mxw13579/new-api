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
	"strings"
	"syscall"
	"time"
	_ "unsafe"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

const (
	liveAuthority   = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	liveBucket      = "invoice-live-private"
	liveETag        = `"invoice-live-etag"`
	ownerToken      = "invoice-live-owner-token-00000001"
	otherToken      = "invoice-live-other-token-00000002"
	reviewerToken   = "invoice-live-review-token-0000003"
	restrictedToken = "invoice-live-denied-token-0000004"
)

var livePDF = []byte("%PDF-1.7\n% deterministic invoice live fixture\n%%EOF\n")

//go:linkname newInvoiceDownloadStore github.com/QuantumNous/new-api/service.newInvoiceDownloadStore
var newInvoiceDownloadStore func() (service.InvoiceObjectStore, error)

type liveObjectStore struct{}

func (liveObjectStore) AuthorityID() string                                         { return liveAuthority }
func (liveObjectStore) Bucket() string                                              { return liveBucket }
func (liveObjectStore) Put(context.Context, string, io.Reader, int64, string) error { return nil }
func (liveObjectStore) Copy(context.Context, string, string) error                  { return nil }
func (liveObjectStore) Head(context.Context, string) (service.InvoiceObjectHead, error) {
	digest := sha256.Sum256(livePDF)
	return service.InvoiceObjectHead{SizeBytes: int64(len(livePDF)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), ETag: liveETag}, nil
}
func (liveObjectStore) Get(_ context.Context, _ string, ifMatch string) (service.InvoiceObjectGet, error) {
	if ifMatch != liveETag {
		return service.InvoiceObjectGet{}, service.ErrInvoiceObjectIntegrityUnavailable
	}
	digest := sha256.Sum256(livePDF)
	return service.InvoiceObjectGet{Body: io.NopCloser(bytes.NewReader(livePDF)), SizeBytes: int64(len(livePDF)), ChecksumSHA256: base64.StdEncoding.EncodeToString(digest[:]), ETag: liveETag}, nil
}
func (liveObjectStore) Delete(context.Context, string) error { return nil }

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
	seedLiveFixture()
	newInvoiceDownloadStore = func() (service.InvoiceObjectStore, error) { return liveObjectStore{}, nil }

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	router.SetApiRouter(engine)
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
		{Username: "invoice-live-owner", Password: "unused", AccessToken: stringPointer(ownerToken), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-owner-aff"},
		{Username: "invoice-live-other", Password: "unused", AccessToken: stringPointer(otherToken), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-other-aff"},
		{Username: "invoice-live-reviewer", Password: "unused", AccessToken: stringPointer(reviewerToken), Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-review-aff"},
		{Username: "invoice-live-restricted", Password: "unused", AccessToken: stringPointer(restrictedToken), Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "live-denied-aff"},
	}
	for index := range users {
		if err := model.DB.Create(&users[index]).Error; err != nil {
			panic(err)
		}
	}
	if err := authz.SetUserPermissions(users[3].Id, authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: false}}); err != nil {
		panic(err)
	}
	now := time.Now().Unix()
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
		ApplicationID: application.ID, R2AuthorityID: stringPointer(liveAuthority), R2Bucket: liveBucket, ObjectKey: &key, ObjectETag: stringPointer(liveETag),
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

func stringPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64    { return &value }
