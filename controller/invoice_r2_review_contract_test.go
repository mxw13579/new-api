package controller

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type invoiceUploadPart struct {
	name        string
	filename    string
	value       string
	data        []byte
	contentType string
}

func invoiceUploadRequest(t *testing.T, parts []invoiceUploadPart) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		if part.filename == "" {
			require.NoError(t, writer.WriteField(part.name, part.value))
			continue
		}
		var file interface{ Write([]byte) (int, error) }
		var err error
		if part.contentType == "" {
			file, err = writer.CreateFormFile(part.name, part.filename)
		} else {
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, part.name, part.filename))
			header.Set("Content-Type", part.contentType)
			file, err = writer.CreatePart(header)
		}
		require.NoError(t, err)
		_, err = file.Write(part.data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/admin/invoices/1/document", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func validInvoiceUploadParts(status string, data []byte) []invoiceUploadPart {
	return []invoiceUploadPart{
		{name: "expected_status", value: status},
		{name: "invoice_number", value: "INV-HTTP-1"},
		{name: "invoice_code", value: "CODE-1"},
		{name: "invoice_date", value: "1721692800"},
		{name: "face_amount_minor", value: "100"},
		{name: "currency", value: constant.InvoiceCurrencyCNY},
		{name: "pdf_facts_attested", value: "true"},
		{name: "file", filename: "invoice.pdf", data: data},
	}
}

func performInvoiceUploadController(request *http.Request) *httptest.ResponseRecorder {
	// A zero actor proves a well-formed request reached the service contract
	// without requiring live R2 credentials or mutating invoice state.
	return performInvoiceUploadControllerAs(request, 0)
}

func invoiceUploadResponseCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload.Data.Code
}

func TestInvoiceUploadHTTPAcceptsInitialAndReplacementShapes(t *testing.T) {
	for _, status := range []string{constant.InvoiceApplicationStatusApproved, constant.InvoiceApplicationStatusIssued} {
		t.Run(status, func(t *testing.T) {
			recorder := performInvoiceUploadController(invoiceUploadRequest(t, validInvoiceUploadParts(status, []byte("%PDF-1.7\n%%EOF"))))

			assert.Equal(t, http.StatusConflict, recorder.Code)
			assert.Equal(t, constant.InvoiceCodeStateConflict, invoiceUploadResponseCode(t, recorder))
		})
	}
}

func TestInvoiceUploadMultipartBoundaryAndCardinality(t *testing.T) {
	tests := []struct {
		name    string
		request func(t *testing.T) *http.Request
		status  int
		code    string
	}{
		{
			name: "ten MiB file passes HTTP limits",
			request: func(t *testing.T) *http.Request {
				return invoiceUploadRequest(t, validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, bytes.Repeat([]byte{'x'}, int(service.InvoicePDFMaxBytes))))
			},
			status: http.StatusConflict, code: constant.InvoiceCodeStateConflict,
		},
		{
			name: "eleven MiB file is rejected",
			request: func(t *testing.T) *http.Request {
				return invoiceUploadRequest(t, validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, bytes.Repeat([]byte{'x'}, int(service.InvoicePDFMaxBytes+(1<<20)))))
			},
			status: http.StatusBadRequest, code: constant.InvoiceCodeInvalidRequest,
		},
		{
			name: "missing file",
			request: func(t *testing.T) *http.Request {
				parts := validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, []byte("pdf"))
				return invoiceUploadRequest(t, parts[:len(parts)-1])
			},
			status: http.StatusBadRequest, code: constant.InvoiceCodeInvalidRequest,
		},
		{
			name: "duplicate file",
			request: func(t *testing.T) *http.Request {
				parts := validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, []byte("pdf"))
				parts = append(parts, invoiceUploadPart{name: "file", filename: "second.pdf", data: []byte("pdf")})
				return invoiceUploadRequest(t, parts)
			},
			status: http.StatusBadRequest, code: constant.InvoiceCodeInvalidRequest,
		},
		{
			name: "duplicate scalar",
			request: func(t *testing.T) *http.Request {
				parts := validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, []byte("pdf"))
				parts = append(parts, invoiceUploadPart{name: "currency", value: constant.InvoiceCurrencyCNY})
				return invoiceUploadRequest(t, parts)
			},
			status: http.StatusBadRequest, code: constant.InvoiceCodeInvalidRequest,
		},
		{
			name: "unknown part",
			request: func(t *testing.T) *http.Request {
				parts := validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, []byte("pdf"))
				parts = append(parts, invoiceUploadPart{name: "object_key", value: "caller-selected"})
				return invoiceUploadRequest(t, parts)
			},
			status: http.StatusBadRequest, code: constant.InvoiceCodeInvalidRequest,
		},
		{
			name: "malformed chunked body",
			request: func(t *testing.T) *http.Request {
				request := httptest.NewRequest(http.MethodPost, "/api/admin/invoices/1/document", strings.NewReader("not multipart"))
				request.ContentLength = -1
				request.TransferEncoding = []string{"chunked"}
				request.Header.Set("Content-Type", "multipart/form-data; boundary=broken")
				return request
			},
			status: http.StatusBadRequest, code: constant.InvoiceCodeInvalidRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performInvoiceUploadController(test.request(t))
			assert.Equal(t, test.status, recorder.Code)
			assert.Equal(t, test.code, invoiceUploadResponseCode(t, recorder))
			assert.NotContains(t, recorder.Body.String(), "caller-selected")
			assert.NotContains(t, recorder.Body.String(), "invoice.pdf")
		})
	}
}

func TestInvoiceUploadHTTPResponseRedactsAllDocumentSentinels(t *testing.T) {
	sentinels := []string{
		"endpoint-http-sentinel-rw2", "bucket-http-sentinel-rw2", "access-key-http-sentinel-rw2",
		"secret-http-sentinel-rw2", "final-key-http-sentinel-rw2", "staging-key-http-sentinel-rw2",
		"provider-http-sentinel-rw2", "filename-http-sentinel-rw2.pdf", "signed-url-http-sentinel-rw2",
		"pdf-bytes-http-sentinel-rw2",
	}
	parts := validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, []byte(sentinels[9]))
	parts[len(parts)-1].filename = sentinels[7]
	parts[1].value = sentinels[4]
	request := invoiceUploadRequest(t, parts)
	request.Header.Set("X-Sentinel-Endpoint", sentinels[0])
	request.Header.Set("X-Sentinel-Bucket", sentinels[1])
	request.Header.Set("X-Sentinel-Access-Key", sentinels[2])
	request.Header.Set("X-Sentinel-Secret", sentinels[3])
	request.Header.Set("X-Sentinel-Staging-Key", sentinels[5])
	request.Header.Set("X-Sentinel-Provider", sentinels[6])
	request.Header.Set("X-Sentinel-Signed-URL", sentinels[8])

	recorder := performInvoiceUploadController(request)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	for _, sentinel := range sentinels {
		assert.NotContains(t, recorder.Body.String(), sentinel)
	}
}

func TestInvoiceUploadHTTPRejectsValidChunkedMultipartBeyondLimit(t *testing.T) {
	request := invoiceUploadRequest(t, validInvoiceUploadParts(
		constant.InvoiceApplicationStatusApproved,
		bytes.Repeat([]byte{'x'}, int(service.InvoicePDFMaxBytes+(1<<20))),
	))
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}

	recorder := performInvoiceUploadController(request)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, constant.InvoiceCodeInvalidRequest, invoiceUploadResponseCode(t, recorder))
}

func TestInvoiceUploadPartContentTypeIsAdvisory(t *testing.T) {
	db, objectCalls := setupInvoiceUploadAdvisoryFixture(t)
	validPDF := buildInvoiceControllerTestPDF()
	_, err := service.ValidateInvoicePDF(bytes.NewReader(validPDF))
	require.NoError(t, err)

	wrongDeclaration := validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, validPDF)
	wrongDeclaration[len(wrongDeclaration)-1].contentType = "text/plain"
	recorder := performInvoiceUploadControllerAs(invoiceUploadRequest(t, wrongDeclaration), 7)
	assert.NotEqual(t, http.StatusBadRequest, recorder.Code, "valid PDF bytes must not be rejected because of the declared part type")
	var accepted model.InvoiceDocument
	require.NoError(t, db.Order("id DESC").First(&accepted).Error)
	assert.Equal(t, model.InvoiceDocumentStatusUploading, accepted.Status)

	objectCallsBeforeInvalid := objectCalls.Load()
	invalidDeclaration := validInvoiceUploadParts(constant.InvoiceApplicationStatusApproved, []byte("not a pdf"))
	invalidDeclaration[len(invalidDeclaration)-1].contentType = model.InvoicePDFContentType
	recorder = performInvoiceUploadControllerAs(invoiceUploadRequest(t, invalidDeclaration), 7)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, constant.InvoiceCodeInvalidRequest, invoiceUploadResponseCode(t, recorder))
	var rejected model.InvoiceDocument
	require.NoError(t, db.Order("id DESC").First(&rejected).Error)
	assert.Equal(t, model.InvoiceDocumentStatusUploadFailed, rejected.Status)
	assert.Equal(t, objectCallsBeforeInvalid, objectCalls.Load(), "invalid PDF bytes must be rejected before object storage")
}

func performInvoiceUploadControllerAs(request *http.Request, actorID int) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Params = gin.Params{{Key: "id", Value: "1"}}
	context.Set("id", actorID)
	AdminUploadInvoiceDocument(context)
	return recorder
}

func setupInvoiceUploadAdvisoryFixture(t *testing.T) (*gorm.DB, *atomic.Int32) {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.InvoiceApplication{}, &model.InvoiceDocument{}))
	model.DB = db
	require.NoError(t, db.Create(&model.InvoiceApplication{
		ID: 1, ApplicationNo: "INV-ADVISORY-1", UserID: 1, RequestID: "advisory-request", RequestFingerprint: strings.Repeat("f", 64),
		Type: constant.InvoiceTypePersonal, Status: constant.InvoiceApplicationStatusApproved,
		PaymentReviewStatus: constant.InvoicePaymentReviewStatusNone, Currency: constant.InvoiceCurrencyCNY,
		AmountMinor: 100, FeeMethod: model.InvoiceFeeMethodWalletQuota, FeeStatus: constant.InvoiceFeeStatusNotRequired,
		ProfileSnapshot: `{}`, PolicySnapshot: `{}`, SubmittedAt: 1,
	}).Error)
	var objectCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		objectCalls.Add(1)
		response.WriteHeader(http.StatusBadRequest)
	}))
	t.Setenv("INVOICE_R2_ENDPOINT", server.URL)
	t.Setenv("INVOICE_R2_BUCKET", "test-fake-private-bucket")
	t.Setenv("INVOICE_R2_ACCESS_KEY_ID", "test-fake-access-key")
	t.Setenv("INVOICE_R2_SECRET_ACCESS_KEY", "test-fake-secret-key")
	t.Cleanup(func() {
		server.Close()
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
		model.DB = previousDB
	})
	return db, &objectCalls
}

func buildInvoiceControllerTestPDF() []byte {
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
