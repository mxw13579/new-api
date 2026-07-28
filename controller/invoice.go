package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

var (
	getInvoiceDocumentDownload = service.GetInvoiceDocumentDownload
	uploadInvoiceDocument      = service.UploadInvoiceDocument
)

func invoiceErrorCode(err error) (string, int) {
	switch {
	case errors.Is(err, model.ErrInvoiceInvalidProfile), errors.Is(err, model.ErrInvoicePaymentSourceInvalidRequest):
		return constant.InvoiceCodeInvalidRequest, http.StatusBadRequest
	case errors.Is(err, service.ErrInvoicePDFTooLarge), errors.Is(err, service.ErrInvoicePDFInvalid),
		errors.Is(err, service.ErrInvoicePDFEncrypted), errors.Is(err, service.ErrInvoicePDFActiveContent):
		return constant.InvoiceCodeInvalidRequest, http.StatusBadRequest
	case errors.Is(err, model.ErrInvoiceQuotaInsufficient):
		return constant.InvoiceCodeQuotaInsufficient, http.StatusForbidden
	case errors.Is(err, model.ErrInvoiceNotFound):
		return constant.InvoiceCodeNotFound, http.StatusNotFound
	case errors.Is(err, model.ErrInvoiceIdempotencyConflict):
		return constant.InvoiceCodeIdempotencyConflict, http.StatusConflict
	case errors.Is(err, model.ErrInvoiceIssuanceConflict):
		return constant.InvoiceCodeIssuanceConflict, http.StatusConflict
	case errors.Is(err, model.ErrInvoiceTopUpIneligible), errors.Is(err, model.ErrInvoicePaymentSourceNotEligible), errors.Is(err, model.ErrInvoicePaymentSourceClaimConflict):
		return constant.InvoiceCodeTopUpIneligible, http.StatusConflict
	case errors.Is(err, model.ErrInvoicePaymentSourceEvidenceConflict), errors.Is(err, model.ErrInvoicePaymentReviewConflict):
		return constant.InvoiceCodePaymentEvidenceConflict, http.StatusConflict
	case errors.Is(err, service.ErrInvoiceDocumentUnavailable):
		return constant.InvoiceCodeDocumentUnavailable, http.StatusConflict
	case errors.Is(err, service.ErrInvoiceR2NotConfigured):
		return constant.InvoiceCodeStorageNotConfigured, http.StatusServiceUnavailable
	case errors.Is(err, model.ErrInvoiceDocumentConflict),
		errors.Is(err, model.ErrInvoiceProfileVersionConflict), errors.Is(err, model.ErrInvoiceStateConflict):
		return constant.InvoiceCodeStateConflict, http.StatusConflict
	default:
		return constant.InvoiceCodeInternalError, http.StatusInternalServerError
	}
}

func writeInvoiceError(c *gin.Context, err error) {
	code, status := invoiceErrorCode(err)
	messageKey := i18n.MsgInvalidParams
	if code == constant.InvoiceCodeStorageNotConfigured {
		messageKey = i18n.MsgInvoiceStorageNotConfigured
	} else if code == constant.InvoiceCodeIssuanceConflict {
		messageKey = i18n.MsgInvoiceIssuanceConflict
	} else if status == http.StatusInternalServerError {
		messageKey = i18n.MsgDatabaseError
	}
	c.JSON(status, gin.H{"success": false, "message": common.TranslateMessage(c, messageKey), "data": gin.H{"code": code}})
}

func writeInvoiceSuccess(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"success": true, "message": "", "data": data})
}

func invoiceApplicationID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, model.ErrInvoicePaymentSourceInvalidRequest
	}
	return id, nil
}

func invoicePage(c *gin.Context) (int, int, error) {
	page, pageSize := 1, 20
	var err error
	if value := c.Query("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, model.ErrInvoicePaymentSourceInvalidRequest
		}
	}
	if value := c.Query("page_size"); value != "" {
		pageSize, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, model.ErrInvoicePaymentSourceInvalidRequest
		}
	}
	if page <= 0 || pageSize <= 0 || pageSize > 100 {
		return 0, 0, model.ErrInvoicePaymentSourceInvalidRequest
	}
	return page, pageSize, nil
}

// GetInvoiceConfig returns the current user-visible invoice policy and currency.
func GetInvoiceConfig(c *gin.Context) {
	writeInvoiceSuccess(c, http.StatusOK, service.GetInvoiceConfig())
}

// ListInvoiceProfiles returns invoice identities owned by the authenticated user.
func ListInvoiceProfiles(c *gin.Context) {
	profiles, err := service.ListInvoiceProfiles(c.GetInt("id"))
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, profiles)
}

// CreateInvoiceProfile creates a personal or company invoice identity for the authenticated user.
func CreateInvoiceProfile(c *gin.Context) {
	var request dto.CreateInvoiceProfileRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoiceError(c, model.ErrInvoiceInvalidProfile)
		return
	}
	profile, err := service.CreateInvoiceProfile(c.GetInt("id"), request)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusCreated, profile)
}

// UpdateInvoiceProfile conditionally updates an owned invoice identity at its expected version.
func UpdateInvoiceProfile(c *gin.Context) {
	var request dto.UpdateInvoiceProfileRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoiceError(c, model.ErrInvoiceInvalidProfile)
		return
	}
	profile, err := service.UpdateInvoiceProfile(c.GetInt("id"), request)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, profile)
}

// DeleteInvoiceProfile conditionally removes an owned invoice identity at its expected version.
func DeleteInvoiceProfile(c *gin.Context) {
	var request dto.DeleteInvoiceProfileRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoiceError(c, model.ErrInvoiceInvalidProfile)
		return
	}
	if err := service.DeleteInvoiceProfile(c.GetInt("id"), request); err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, nil)
}

// ListEligibleInvoiceOrders returns the authenticated user's paid orders that remain invoiceable.
func ListEligibleInvoiceOrders(c *gin.Context) {
	page, pageSize, err := invoicePage(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	result, err := service.ListEligibleInvoiceOrders(c.GetInt("id"), page, pageSize)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, result)
}

// CreateInvoiceApplication submits an idempotent invoice request for owned eligible orders.
func CreateInvoiceApplication(c *gin.Context) {
	var request dto.CreateInvoiceApplicationRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	detail, err := service.CreateInvoiceApplicationDetail(c.GetInt("id"), request)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusCreated, detail)
}

// ListInvoiceApplications returns paginated invoice applications owned by the authenticated user.
func ListInvoiceApplications(c *gin.Context) {
	page, pageSize, err := invoicePage(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	ownerID := c.GetInt("id")
	result, err := service.ListInvoiceApplicationPage(&ownerID, page, pageSize, false)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, result)
}

// ListInvoiceFeeHistory returns the authenticated owner's invoice-fee ledger page.
func ListInvoiceFeeHistory(c *gin.Context) {
	page, pageSize, err := invoicePage(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	ownerID := c.GetInt("id")
	result, err := service.ListInvoiceFeeHistory(&ownerID, page, pageSize)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, result)
}

// AdminListInvoiceFeeHistory returns the global invoice-fee ledger for authorized reviewers.
func AdminListInvoiceFeeHistory(c *gin.Context) {
	page, pageSize, err := invoicePage(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	result, err := service.ListInvoiceFeeHistory(nil, page, pageSize)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, result)
}

// GetInvoiceApplication returns an owned invoice application with immutable item and lifecycle details.
func GetInvoiceApplication(c *gin.Context) {
	id, err := invoiceApplicationID(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	ownerID := c.GetInt("id")
	detail, err := service.GetInvoiceApplicationDetail(id, &ownerID, true, false)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, detail)
}

// DownloadInvoiceDocument returns a fully verified owner-scoped PDF without exposing object-store URLs.
func DownloadInvoiceDocument(c *gin.Context) {
	id, err := invoiceApplicationID(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	content, err := getInvoiceDocumentDownload(c.Request.Context(), c.GetInt("id"), id)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Header("Content-Disposition", `attachment; filename="invoice.pdf"`)
	c.Header("Content-Length", strconv.Itoa(len(content)))
	c.Data(http.StatusOK, model.InvoicePDFContentType, content)
}

// CancelInvoiceApplication cancels an owned submitted invoice application and returns its updated detail.
func CancelInvoiceApplication(c *gin.Context) {
	id, err := invoiceApplicationID(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	detail, err := service.CancelInvoiceApplicationDetail(c.GetInt("id"), id)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, detail)
}

// AdminListInvoiceApplications returns invoice applications across users for authorized review.
func AdminListInvoiceApplications(c *gin.Context) {
	page, pageSize, err := invoicePage(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	result, err := service.ListInvoiceApplicationPage(nil, page, pageSize, true)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, result)
}

// AdminGetInvoiceApplication returns an invoice application with tax data gated by sensitive-read permission.
func AdminGetInvoiceApplication(c *gin.Context) {
	id, err := invoiceApplicationID(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	includeSensitive := authz.Can(c.GetInt("id"), c.GetInt("role"), authz.InvoiceSensitiveRead)
	detail, err := service.GetInvoiceApplicationDetail(id, nil, includeSensitive, true)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, detail)
}

// AdminReviewInvoiceApplication advances an invoice through the authorized review state transition.
func AdminReviewInvoiceApplication(c *gin.Context) {
	id, err := invoiceApplicationID(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	var request dto.ReviewInvoiceApplicationRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	application, err := service.ReviewInvoiceApplication(c.GetInt("id"), id, request)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	includeSensitive := authz.Can(c.GetInt("id"), c.GetInt("role"), authz.InvoiceSensitiveRead)
	detail, err := service.GetInvoiceApplicationDetail(application.ID, nil, includeSensitive, true)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, detail)
}

// AdminRejectInvoiceApplication rejects an invoice at its expected state with an operator reason.
func AdminRejectInvoiceApplication(c *gin.Context) {
	id, err := invoiceApplicationID(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	var request dto.RejectInvoiceApplicationRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	application, err := service.RejectInvoiceApplication(c.GetInt("id"), id, request)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	includeSensitive := authz.Can(c.GetInt("id"), c.GetInt("role"), authz.InvoiceSensitiveRead)
	detail, err := service.GetInvoiceApplicationDetail(application.ID, nil, includeSensitive, true)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, detail)
}

// AdminUploadInvoiceDocument validates and attaches an attested PDF to an approved or issued invoice.
func AdminUploadInvoiceDocument(c *gin.Context) {
	id, err := invoiceApplicationID(c)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.InvoicePDFMaxBytes+(1<<20))
	if err := c.Request.ParseMultipartForm(service.InvoicePDFMaxBytes + (1 << 20)); err != nil || c.Request.MultipartForm == nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	allowedValues := map[string]bool{
		"expected_status": true, "invoice_number": true, "invoice_code": true, "invoice_date": true,
		"face_amount_minor": true, "currency": true, "pdf_facts_attested": true,
	}
	for key, values := range c.Request.MultipartForm.Value {
		if !allowedValues[key] || len(values) != 1 {
			writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
			return
		}
	}
	for key, files := range c.Request.MultipartForm.File {
		if key != "file" || len(files) != 1 {
			writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
			return
		}
	}
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size <= 0 || fileHeader.Size > service.InvoicePDFMaxBytes {
		writeInvoiceError(c, service.ErrInvoicePDFInvalid)
		return
	}
	invoiceDate, err := strconv.ParseInt(c.PostForm("invoice_date"), 10, 64)
	if err != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	faceAmountMinor, err := strconv.ParseInt(c.PostForm("face_amount_minor"), 10, 64)
	if err != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	attested, err := strconv.ParseBool(c.PostForm("pdf_facts_attested"))
	if err != nil {
		writeInvoiceError(c, model.ErrInvoicePaymentSourceInvalidRequest)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeInvoiceError(c, service.ErrInvoicePDFInvalid)
		return
	}
	defer file.Close()
	request := dto.InvoiceDocumentUploadRequest{
		ExpectedStatus: c.PostForm("expected_status"), InvoiceNumber: c.PostForm("invoice_number"), InvoiceCode: c.PostForm("invoice_code"),
		InvoiceDate: invoiceDate, FaceAmountMinor: faceAmountMinor, Currency: c.PostForm("currency"), PDFFactsAttested: attested,
	}
	if err := uploadInvoiceDocument(c.Request.Context(), c.GetInt("id"), id, request, file); err != nil {
		writeInvoiceError(c, err)
		return
	}
	includeSensitive := authz.Can(c.GetInt("id"), c.GetInt("role"), authz.InvoiceSensitiveRead)
	includeIdentity := authz.Can(c.GetInt("id"), c.GetInt("role"), authz.InvoiceReview)
	detail, err := service.GetInvoiceApplicationDetail(id, nil, includeSensitive, includeIdentity)
	if err != nil {
		writeInvoiceError(c, err)
		return
	}
	writeInvoiceSuccess(c, http.StatusOK, detail)
}
