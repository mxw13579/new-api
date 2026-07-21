package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func writeInvoicePaymentEvidenceError(c *gin.Context, err error) {
	code := service.InvoicePaymentEvidenceErrorCode(err)
	status := http.StatusInternalServerError
	switch code {
	case constant.InvoicePaymentEvidenceCodeInvalidRequest:
		status = http.StatusBadRequest
	case constant.InvoicePaymentEvidenceCodeRunNotFound:
		status = http.StatusNotFound
	case constant.InvoicePaymentEvidenceCodePolicyMismatch,
		constant.InvoicePaymentEvidenceCodeRunStateConflict,
		constant.InvoicePaymentEvidenceCodeCutoverNotReady:
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"success": false, "code": code, "message": service.InvoicePaymentEvidenceSafeMessage(err)})
}

func invoicePaymentEvidenceRunID(c *gin.Context) (int64, error) {
	runID, err := strconv.ParseInt(c.Param("run_id"), 10, 64)
	if err != nil || runID <= 0 {
		return 0, &service.InvoicePaymentEvidenceError{Code: constant.InvoicePaymentEvidenceCodeInvalidRequest, SafeMessage: "invalid request"}
	}
	return runID, nil
}

func PreviewInvoicePaymentEvidenceBackfill(c *gin.Context) {
	var request dto.BackfillPreviewRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil {
		writeInvoicePaymentEvidenceError(c, &service.InvoicePaymentEvidenceError{Code: constant.InvoicePaymentEvidenceCodeInvalidRequest, SafeMessage: "invalid request"})
		return
	}
	response, created, err := service.PreviewInvoicePaymentEvidenceBackfill(c.Request.Context(), c.GetInt("id"), request)
	if err != nil {
		writeInvoicePaymentEvidenceError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"success": true, "message": "", "data": response})
}

func ApplyInvoicePaymentEvidenceBackfill(c *gin.Context) {
	runID, err := invoicePaymentEvidenceRunID(c)
	if err != nil {
		writeInvoicePaymentEvidenceError(c, err)
		return
	}
	var request dto.BackfillApplyRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, false); err != nil || request.ExpectedPolicySHA256 == "" {
		writeInvoicePaymentEvidenceError(c, &service.InvoicePaymentEvidenceError{Code: constant.InvoicePaymentEvidenceCodeInvalidRequest, SafeMessage: "invalid request"})
		return
	}
	response, err := service.ApplyInvoicePaymentEvidenceBackfill(c.Request.Context(), c.GetInt("id"), runID, request)
	if err != nil {
		writeInvoicePaymentEvidenceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "message": "", "data": response})
}

func GetInvoicePaymentEvidenceBackfill(c *gin.Context) {
	runID, err := invoicePaymentEvidenceRunID(c)
	if err != nil {
		writeInvoicePaymentEvidenceError(c, err)
		return
	}
	response, err := service.GetInvoicePaymentEvidenceBackfill(c.Request.Context(), runID)
	if err != nil {
		writeInvoicePaymentEvidenceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}

func StopInvoicePaymentEvidenceBackfill(c *gin.Context) {
	runID, err := invoicePaymentEvidenceRunID(c)
	if err != nil {
		writeInvoicePaymentEvidenceError(c, err)
		return
	}
	var request dto.BackfillStopRequest
	if err := common.DecodeStrictJSONObject(c.Request.Body, &request, true); err != nil {
		writeInvoicePaymentEvidenceError(c, &service.InvoicePaymentEvidenceError{Code: constant.InvoicePaymentEvidenceCodeInvalidRequest, SafeMessage: "invalid request"})
		return
	}
	response, err := service.StopInvoicePaymentEvidenceBackfill(c.Request.Context(), c.GetInt("id"), runID)
	if err != nil {
		writeInvoicePaymentEvidenceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}
