package router

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceAdminRoutesUseIndependentReviewAndDocumentPermissions(t *testing.T) {
	expected := map[string]authz.Permission{
		http.MethodPost + " /invoices/:id/review":   authz.InvoiceReview,
		http.MethodPost + " /invoices/:id/reject":   authz.InvoiceReview,
		http.MethodPost + " /invoices/:id/document": authz.InvoiceDocumentUpload,
	}

	for _, route := range invoiceAdminRoutes {
		permission, ok := expected[route.method+" "+route.path]
		if !ok {
			continue
		}
		require.NotNil(t, route.permission)
		assert.Equal(t, permission, *route.permission)
		delete(expected, route.method+" "+route.path)
	}
	assert.Empty(t, expected)
}

type invoiceReviewRouteFixture struct {
	engine       *gin.Engine
	db           *gorm.DB
	adminID      int
	adminToken   string
	domainWrites atomic.Int32
	objectCalls  atomic.Int32
}

func setupInvoiceReviewRouteFixture(t *testing.T) *invoiceReviewRouteFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	previousMainType := common.MainDatabaseType()
	common.RedisEnabled = false
	common.IsMasterNode = true
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	databaseName := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", databaseName)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	logDB, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s_logs?mode=memory&cache=shared", databaseName)), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.CasbinRule{}, &model.AuthzRole{},
		&model.InvoiceApplication{}, &model.InvoiceIssuance{}, &model.InvoiceDocument{},
	))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	model.DB, model.LOG_DB = db, logDB
	require.NoError(t, authz.Init(db))

	fixture := &invoiceReviewRouteFixture{db: db, adminID: 9101, adminToken: strings.Repeat("d", 32)}
	admin := &model.User{
		Id: fixture.adminID, Username: "invoice-route-denied", Password: "test-password",
		AccessToken: &fixture.adminToken, Role: common.RoleAdminUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "invoice-route-denied", AuthVersion: 1,
	}
	require.NoError(t, db.Create(admin).Error)
	require.NoError(t, authz.SetUserPermissions(fixture.adminID, authz.PermissionsMap{
		authz.ResourceInvoice: {
			authz.ActionInvoiceReview:         false,
			authz.ActionInvoiceDocumentUpload: false,
		},
	}))
	seedInvoiceReviewRouteApplications(t, db)

	callbackPrefix := "test:invoice-review-route-domain-write"
	countDomainWrite := func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.Table, "invoice_") {
			fixture.domainWrites.Add(1)
		}
	}
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackPrefix+":create", countDomainWrite))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackPrefix+":update", countDomainWrite))
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callbackPrefix+":delete", countDomainWrite))

	objectServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		fixture.objectCalls.Add(1)
	}))
	t.Setenv("INVOICE_R2_ENDPOINT", objectServer.URL)
	t.Setenv("INVOICE_R2_BUCKET", "test-fake-private-bucket")
	t.Setenv("INVOICE_R2_ACCESS_KEY_ID", "test-fake-access-key")
	t.Setenv("INVOICE_R2_SECRET_ACCESS_KEY", "test-fake-secret-key")

	fixture.engine = gin.New()
	SetApiRouter(fixture.engine)
	t.Cleanup(func() {
		objectServer.Close()
		_ = db.Callback().Create().Remove(callbackPrefix + ":create")
		_ = db.Callback().Update().Remove(callbackPrefix + ":update")
		_ = db.Callback().Delete().Remove(callbackPrefix + ":delete")
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.IsMasterNode = previousRedis, previousMaster
		common.SetMainDatabaseType(previousMainType)
	})
	return fixture
}

func TestInvoiceAdminPermissionDenialsUseProductionAuthenticationAndHaveNoDomainSideEffects(t *testing.T) {
	fixture := setupInvoiceReviewRouteFixture(t)
	tests := []struct {
		name        string
		path        string
		contentType string
		body        []byte
	}{
		{name: "approve", path: "/api/admin/invoices/9201/review", contentType: "application/json", body: []byte(`{"action":"approve","expected_status":"submitted"}`)},
		{name: "reject", path: "/api/admin/invoices/9202/reject", contentType: "application/json", body: []byte(`{"expected_status":"submitted","reason":"test rejection"}`)},
		invoiceReviewUploadRouteCase(t, "initial upload", 9203, constant.InvoiceApplicationStatusApproved),
		invoiceReviewUploadRouteCase(t, "replacement upload", 9204, constant.InvoiceApplicationStatusIssued),
	}

	var baselineApplications []model.InvoiceApplication
	require.NoError(t, fixture.db.Order("id").Find(&baselineApplications).Error)
	for _, testCase := range tests {
		for _, identity := range []struct {
			name       string
			authorized bool
			status     int
			code       string
		}{
			{name: "unauthenticated", status: http.StatusUnauthorized, code: "AUTH_UNAUTHORIZED"},
			{name: "authenticated permission denied", authorized: true, status: http.StatusForbidden, code: constant.InvoiceCodeForbidden},
		} {
			t.Run(testCase.name+"/"+identity.name, func(t *testing.T) {
				var bodyReads atomic.Int32
				request := httptest.NewRequest(http.MethodPost, testCase.path, &invoiceReviewRouteReadSentinel{
					reader: bytes.NewReader(testCase.body), reads: &bodyReads,
				})
				request.Header.Set("Content-Type", testCase.contentType)
				if identity.authorized {
					request.Header.Set("Authorization", "Bearer "+fixture.adminToken)
					request.Header.Set("New-Api-User", fmt.Sprint(fixture.adminID))
				}
				recorder := httptest.NewRecorder()
				fixture.engine.ServeHTTP(recorder, request)

				assert.Equal(t, identity.status, recorder.Code)
				if identity.status == http.StatusUnauthorized {
					var response struct {
						Code string `json:"code"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					assert.Equal(t, identity.code, response.Code)
				} else {
					var response struct {
						Data struct {
							Code string `json:"code"`
						} `json:"data"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					assert.Equal(t, identity.code, response.Data.Code)
				}
				assert.Zero(t, bodyReads.Load(), "invoice handler must not consume the request body")
				assert.Zero(t, fixture.domainWrites.Load(), "invoice service must not mutate domain tables")
				assert.Zero(t, fixture.objectCalls.Load(), "invoice service must not contact the object store")
				var applications []model.InvoiceApplication
				require.NoError(t, fixture.db.Order("id").Find(&applications).Error)
				assert.Equal(t, baselineApplications, applications)
			})
		}
	}
}

func TestInvoiceAdminProductionPermissionMatrix(t *testing.T) {
	identities := []struct {
		name          string
		role          int
		permissions   authz.PermissionsMap
		authenticated bool
		reviewOK      bool
		uploadOK      bool
		deniedCode    string
	}{
		{name: "unauthenticated", role: common.RoleAdminUser, deniedCode: "AUTH_UNAUTHORIZED"},
		{name: "no target permission", role: common.RoleCommonUser, authenticated: true, deniedCode: "AUTH_INSUFFICIENT_PRIVILEGE"},
		{name: "review only", role: common.RoleAdminUser, authenticated: true, permissions: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: true, authz.ActionInvoiceDocumentUpload: false}}, reviewOK: true, deniedCode: constant.InvoiceCodeForbidden},
		{name: "upload only", role: common.RoleAdminUser, authenticated: true, permissions: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: false, authz.ActionInvoiceDocumentUpload: true}}, uploadOK: true, deniedCode: constant.InvoiceCodeForbidden},
		{name: "explicit deny", role: common.RoleAdminUser, authenticated: true, permissions: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: false, authz.ActionInvoiceDocumentUpload: false}}, deniedCode: constant.InvoiceCodeForbidden},
		{name: "explicit allow", role: common.RoleAdminUser, authenticated: true, permissions: authz.PermissionsMap{authz.ResourceInvoice: {authz.ActionInvoiceReview: true, authz.ActionInvoiceDocumentUpload: true}}, reviewOK: true, uploadOK: true},
	}

	for _, identity := range identities {
		t.Run(identity.name, func(t *testing.T) {
			fixture := setupInvoiceReviewRouteFixture(t)
			require.NoError(t, fixture.db.Model(&model.User{}).Where("id = ?", fixture.adminID).Update("role", identity.role).Error)
			require.NoError(t, authz.ClearUserPermissions(fixture.adminID))
			if identity.permissions != nil {
				require.NoError(t, authz.SetUserPermissions(fixture.adminID, identity.permissions))
			}

			for _, testCase := range []struct {
				name        string
				path        string
				contentType string
				body        []byte
				allowed     bool
			}{
				{name: "approve", path: "/api/admin/invoices/9201/review", contentType: "application/json", body: []byte(`{}`), allowed: identity.reviewOK},
				{name: "reject", path: "/api/admin/invoices/9202/reject", contentType: "application/json", body: []byte(`{}`), allowed: identity.reviewOK},
				invoiceReviewUploadMatrixCase(t, "initial", 9203, constant.InvoiceApplicationStatusApproved, identity.uploadOK),
				invoiceReviewUploadMatrixCase(t, "replacement", 9204, constant.InvoiceApplicationStatusIssued, identity.uploadOK),
			} {
				t.Run(testCase.name, func(t *testing.T) {
					beforeWrites, beforeObjects := fixture.domainWrites.Load(), fixture.objectCalls.Load()
					var beforeApplications []model.InvoiceApplication
					require.NoError(t, fixture.db.Order("id").Find(&beforeApplications).Error)
					var bodyReads atomic.Int32
					request := httptest.NewRequest(http.MethodPost, testCase.path, &invoiceReviewRouteReadSentinel{
						reader: bytes.NewReader(testCase.body), reads: &bodyReads,
					})
					request.Header.Set("Content-Type", testCase.contentType)
					if identity.authenticated {
						request.Header.Set("Authorization", "Bearer "+fixture.adminToken)
						request.Header.Set("New-Api-User", fmt.Sprint(fixture.adminID))
					}
					recorder := httptest.NewRecorder()
					fixture.engine.ServeHTTP(recorder, request)

					if testCase.allowed {
						assert.NotEqual(t, http.StatusUnauthorized, recorder.Code)
						assert.NotEqual(t, http.StatusForbidden, recorder.Code)
						assert.Positive(t, bodyReads.Load(), "allowed production route must invoke its handler")
						return
					}
					if identity.authenticated {
						assert.Equal(t, http.StatusForbidden, recorder.Code)
					} else {
						assert.Equal(t, http.StatusUnauthorized, recorder.Code)
					}
					assert.Equal(t, identity.deniedCode, invoiceReviewDeniedCode(t, recorder, identity.role >= common.RoleAdminUser && identity.authenticated))
					assert.Zero(t, bodyReads.Load(), "denied production route must not invoke its handler")
					assert.Equal(t, beforeWrites, fixture.domainWrites.Load(), "denied route must not mutate invoice domain tables")
					assert.Equal(t, beforeObjects, fixture.objectCalls.Load(), "denied route must not contact object storage")
					var afterApplications []model.InvoiceApplication
					require.NoError(t, fixture.db.Order("id").Find(&afterApplications).Error)
					assert.Equal(t, beforeApplications, afterApplications)
				})
			}
		})
	}
}

func invoiceReviewUploadMatrixCase(t *testing.T, name string, applicationID int64, status string, allowed bool) struct {
	name        string
	path        string
	contentType string
	body        []byte
	allowed     bool
} {
	t.Helper()
	testCase := invoiceReviewUploadRouteCase(t, name, applicationID, status)
	return struct {
		name        string
		path        string
		contentType string
		body        []byte
		allowed     bool
	}{name: testCase.name, path: testCase.path, contentType: testCase.contentType, body: testCase.body, allowed: allowed}
}

func invoiceReviewDeniedCode(t *testing.T, recorder *httptest.ResponseRecorder, invoicePermissionDenial bool) string {
	t.Helper()
	if invoicePermissionDenial {
		var response struct {
			Data struct {
				Code string `json:"code"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return response.Data.Code
	}
	var response struct {
		Code string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Code
}

type invoiceReviewRouteReadSentinel struct {
	reader io.Reader
	reads  *atomic.Int32
}

func (sentinel *invoiceReviewRouteReadSentinel) Read(buffer []byte) (int, error) {
	sentinel.reads.Add(1)
	return sentinel.reader.Read(buffer)
}

func invoiceReviewUploadRouteCase(t *testing.T, name string, applicationID int64, status string) struct {
	name        string
	path        string
	contentType string
	body        []byte
} {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("expected_status", status))
	require.NoError(t, writer.WriteField("invoice_number", "INV-ROUTE-SENTINEL"))
	require.NoError(t, writer.WriteField("invoice_date", "100"))
	require.NoError(t, writer.WriteField("face_amount_minor", "500"))
	require.NoError(t, writer.WriteField("currency", constant.InvoiceCurrencyCNY))
	require.NoError(t, writer.WriteField("pdf_facts_attested", "true"))
	file, err := writer.CreateFormFile("file", "invoice.pdf")
	require.NoError(t, err)
	_, err = file.Write([]byte("%PDF-1.7\n%%EOF"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return struct {
		name        string
		path        string
		contentType string
		body        []byte
	}{name: name, path: fmt.Sprintf("/api/admin/invoices/%d/document", applicationID), contentType: writer.FormDataContentType(), body: body.Bytes()}
}

func seedInvoiceReviewRouteApplications(t *testing.T, db *gorm.DB) {
	t.Helper()
	statuses := map[int64]string{
		9201: constant.InvoiceApplicationStatusSubmitted,
		9202: constant.InvoiceApplicationStatusSubmitted,
		9203: constant.InvoiceApplicationStatusApproved,
		9204: constant.InvoiceApplicationStatusIssued,
	}
	for id, status := range statuses {
		application := &model.InvoiceApplication{
			ID: id, ApplicationNo: fmt.Sprintf("INV-ROUTE-%d", id), UserID: 1,
			RequestID: fmt.Sprintf("invoice-route-%d", id), RequestFingerprint: strings.Repeat("f", 64),
			Type: constant.InvoiceTypePersonal, Status: status, PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone,
			Currency: constant.InvoiceCurrencyCNY, AmountMinor: 500, FeeMethod: model.InvoiceFeeMethodWalletQuota,
			FeeStatus: constant.InvoiceFeeStatusNotRequired, ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
		}
		require.NoError(t, db.Create(application).Error)
	}
	issuance := &model.InvoiceIssuance{
		ApplicationID: 9204, InvoiceNumber: "INV-ROUTE-SENTINEL", InvoiceDate: 100,
		FaceAmountMinor: 500, Currency: constant.InvoiceCurrencyCNY, CreatedBy: 9101, CreatedAt: 100, UpdatedAt: 100,
	}
	require.NoError(t, db.Create(issuance).Error)
	objectKey := "invoices/route-replacement.pdf"
	version, actor, availableAt, expiresAt := int64(1), 9101, int64(100), int64(100+30*86400)
	document := &model.InvoiceDocument{
		ID: 9304, ApplicationID: 9204, IssuanceID: &issuance.ID, Version: &version,
		R2Bucket: "test-fake-private-bucket", ObjectKey: &objectKey, ContentType: model.InvoicePDFContentType,
		SizeBytes: 100, SHA256: strings.Repeat("a", 64), Status: model.InvoiceDocumentStatusAvailable,
		OperationToken: strings.Repeat("b", 64), OperationStartedAt: 100, UploadedBy: actor, UploadedAt: 100,
		PDFFactsAttested: true, AttestedBy: &actor, AttestedAt: &availableAt,
		AttestedProfileSnapshotSHA256: strings.Repeat("c", 64), AvailableAt: &availableAt,
		RetentionDaysSnapshot: 30, ExpiresAt: &expiresAt, CreatedAt: 100, UpdatedAt: 100,
	}
	require.NoError(t, db.Create(document).Error)
	require.NoError(t, db.Model(&model.InvoiceApplication{}).Where("id = ?", 9204).
		Updates(map[string]any{"active_document_id": document.ID, "issued_at": availableAt}).Error)
}
