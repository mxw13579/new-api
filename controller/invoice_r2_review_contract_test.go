package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type invoiceUploadPart struct {
	name     string
	filename string
	value    string
	data     []byte
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
		file, err := writer.CreateFormFile(part.name, part.filename)
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
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Params = gin.Params{{Key: "id", Value: "1"}}
	// A zero actor proves a well-formed request reached the service contract
	// without requiring live R2 credentials or mutating invoice state.
	context.Set("id", 0)
	AdminUploadInvoiceDocument(context)
	return recorder
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
