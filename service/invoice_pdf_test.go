package service

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildInvoiceTestPDF(t *testing.T, catalogExtra string, extraObjects ...string) []byte {
	t.Helper()
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
	}
	objects = append(objects, extraObjects...)
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

func TestValidateInvoicePDFAcceptsStructuralPDFAndBenignSignature(t *testing.T) {
	valid := buildInvoiceTestPDF(t, "/AcroForm << /Fields [5 0 R] >>",
		"<< /FT /Sig /T (administrator signature) /Rect [0 0 10 10] /V 6 0 R >>",
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /Contents <00> >>",
	)

	result, err := ValidateInvoicePDF(bytes.NewReader(valid))
	require.NoError(t, err)
	assert.Equal(t, int64(len(valid)), result.SizeBytes)
	assert.Len(t, result.SHA256, 64)

	benign := buildInvoiceTestPDF(t, "", "(text mentioning /JavaScript and /Launch is not an action)")
	_, err = ValidateInvoicePDF(bytes.NewReader(benign))
	require.NoError(t, err)
}

func TestValidateInvoicePDFRejectsDamagedEncryptedAndActiveContent(t *testing.T) {
	_, err := ValidateInvoicePDF(bytes.NewReader([]byte("not a pdf")))
	assert.ErrorIs(t, err, ErrInvoicePDFInvalid)

	var encrypted bytes.Buffer
	require.NoError(t, api.Encrypt(bytes.NewReader(buildInvoiceTestPDF(t, "")), &encrypted, pdfmodel.NewAESConfiguration("user", "owner", 256)))
	_, err = ValidateInvoicePDF(bytes.NewReader(encrypted.Bytes()))
	assert.ErrorIs(t, err, ErrInvoicePDFEncrypted)

	tests := []struct {
		name         string
		catalogExtra string
		extra        []string
	}{
		{name: "javascript name tree", catalogExtra: "/Names << /JavaScript << /Names [(run) 5 0 R] >> >>", extra: []string{"<< /S /JavaScript /JS (app.alert('x')) >>"}},
		{name: "launch action", catalogExtra: "/OpenAction 5 0 R", extra: []string{"<< /S /Launch /F (payload.exe) >>"}},
		{name: "embedded file", catalogExtra: "/Names << /EmbeddedFiles << /Names [(payload) 5 0 R] >> >>", extra: []string{"<< /Type /Filespec /F (payload.bin) /EF << /F 6 0 R >> >>", "<< /Type /EmbeddedFile /Length 0 >>\nstream\n\nendstream"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ValidateInvoicePDF(bytes.NewReader(buildInvoiceTestPDF(t, testCase.catalogExtra, testCase.extra...)))
			assert.ErrorIs(t, err, ErrInvoicePDFActiveContent)
		})
	}

	oversized := bytes.NewReader(bytes.Repeat([]byte{'x'}, int(InvoicePDFMaxBytes+1)))
	_, err = ValidateInvoicePDF(oversized)
	assert.ErrorIs(t, err, ErrInvoicePDFTooLarge)
}

func TestValidateInvoicePDFRejectsFrozenActionTypesAndNextChains(t *testing.T) {
	for _, action := range []string{"JavaScript", "Launch", "URI", "GoToR", "GoToE", "SubmitForm", "ImportData", "Rendition", "RichMediaExecute"} {
		t.Run(action, func(t *testing.T) {
			traversal := invoicePDFTraversal{indirect: map[string]struct{}{}, compound: map[uintptr]struct{}{}}
			err := traversal.walk(types.Dict{"S": types.Name(action)}, 0, false, invoicePDFLocationAction)
			assert.ErrorIs(t, err, ErrInvoicePDFActiveContent)
		})
	}

	traversal := invoicePDFTraversal{indirect: map[string]struct{}{}, compound: map[uintptr]struct{}{}}
	chained := types.Dict{"S": types.Name("GoTo"), "Next": types.Dict{"S": types.Name("URI")}}
	err := traversal.walk(chained, 0, false, invoicePDFLocationAction)
	assert.ErrorIs(t, err, ErrInvoicePDFActiveContent)
}

func TestValidateInvoicePDFAllowsBenignNamesAndSafeActions(t *testing.T) {
	traversal := invoicePDFTraversal{indirect: map[string]struct{}{}, compound: map[uintptr]struct{}{}}
	benign := types.Dict{"Metadata": types.Dict{"S": types.Name("JavaScript"), "URI": types.StringLiteral("ordinary URI text")}}
	err := traversal.walk(benign, 0, false, invoicePDFLocationGeneric)
	require.NoError(t, err)

	safeAction := buildInvoiceTestPDF(t, "/OpenAction 5 0 R", "<< /S /GoTo /D [3 0 R /Fit] >>")
	_, err = ValidateInvoicePDF(bytes.NewReader(safeAction))
	require.NoError(t, err)
}

func TestInvoicePDFTraversalBudgetDepthAndNodes(t *testing.T) {
	atDepth := types.Object(types.Dict{})
	for range invoicePDFMaxDepth {
		atDepth = types.Array{atDepth}
	}
	require.NoError(t, rejectInvoicePDFActiveGraph(nil, atDepth))

	deep := types.Object(types.Dict{})
	for range invoicePDFMaxDepth + 1 {
		deep = types.Array{deep}
	}
	assert.ErrorIs(t, rejectInvoicePDFActiveGraph(nil, deep), ErrInvoicePDFInvalid)

	atLimit := make(types.Array, invoicePDFMaxNodes)
	for index := range atLimit {
		atLimit[index] = types.Integer(index)
	}
	require.NoError(t, rejectInvoicePDFActiveGraph(nil, atLimit))
	overLimit := append(atLimit, types.Integer(invoicePDFMaxNodes))
	assert.ErrorIs(t, rejectInvoicePDFActiveGraph(nil, overLimit), ErrInvoicePDFInvalid)

	cycle := types.Dict{}
	cycle["Metadata"] = cycle
	require.NoError(t, rejectInvoicePDFActiveGraph(nil, cycle))
}

func TestValidateInvoicePDFAcceptsExactTenMiBAndRejectsOneByteOver(t *testing.T) {
	padding := int(InvoicePDFMaxBytes) - len(buildInvoiceTestPDF(t, ""))
	var exact []byte
	for range 4 {
		extra := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", padding, strings.Repeat(" ", padding))
		exact = buildInvoiceTestPDF(t, "", extra)
		padding += int(InvoicePDFMaxBytes) - len(exact)
	}
	require.Len(t, exact, int(InvoicePDFMaxBytes))
	result, err := ValidateInvoicePDF(bytes.NewReader(exact))
	require.NoError(t, err)
	assert.Equal(t, InvoicePDFMaxBytes, result.SizeBytes)

	_, err = ValidateInvoicePDF(bytes.NewReader(append(exact, 0)))
	require.ErrorIs(t, err, ErrInvoicePDFTooLarge)
}
