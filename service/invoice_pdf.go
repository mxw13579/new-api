package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const InvoicePDFMaxBytes int64 = 10 << 20

var (
	ErrInvoicePDFTooLarge      = errors.New("invoice pdf exceeds size limit")
	ErrInvoicePDFInvalid       = errors.New("invoice pdf is invalid")
	ErrInvoicePDFEncrypted     = errors.New("encrypted invoice pdf is not allowed")
	ErrInvoicePDFActiveContent = errors.New("active invoice pdf content is not allowed")
)

type InvoicePDFValidation struct {
	SizeBytes int64
	SHA256    string
}

func ValidateInvoicePDF(reader io.Reader) (InvoicePDFValidation, error) {
	if reader == nil {
		return InvoicePDFValidation{}, ErrInvoicePDFInvalid
	}
	limited := io.LimitReader(reader, InvoicePDFMaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return InvoicePDFValidation{}, fmt.Errorf("%w: read failed", ErrInvoicePDFInvalid)
	}
	if int64(len(data)) > InvoicePDFMaxBytes {
		return InvoicePDFValidation{}, ErrInvoicePDFTooLarge
	}
	if len(data) == 0 {
		return InvoicePDFValidation{}, ErrInvoicePDFInvalid
	}

	conf := pdfmodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfmodel.ValidationRelaxed
	ctx, err := api.ReadContext(bytes.NewReader(data), conf)
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "password") || strings.Contains(message, "encrypted") {
			return InvoicePDFValidation{}, ErrInvoicePDFEncrypted
		}
		return InvoicePDFValidation{}, fmt.Errorf("%w: parse failed", ErrInvoicePDFInvalid)
	}
	if ctx.Encrypt != nil {
		return InvoicePDFValidation{}, ErrInvoicePDFEncrypted
	}
	if err := api.ValidateContext(ctx); err != nil {
		return InvoicePDFValidation{}, fmt.Errorf("%w: structural validation failed", ErrInvoicePDFInvalid)
	}
	root, err := ctx.Catalog()
	if err != nil {
		return InvoicePDFValidation{}, fmt.Errorf("%w: catalog unavailable", ErrInvoicePDFInvalid)
	}
	if err := rejectInvoicePDFActiveGraph(ctx, root, map[string]struct{}{}); err != nil {
		return InvoicePDFValidation{}, err
	}

	digest := sha256.Sum256(data)
	return InvoicePDFValidation{SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}, nil
}

func rejectInvoicePDFActiveGraph(ctx *pdfmodel.Context, object types.Object, visited map[string]struct{}) error {
	if reference, ok := object.(types.IndirectRef); ok {
		key := reference.PDFString()
		if _, seen := visited[key]; seen {
			return nil
		}
		visited[key] = struct{}{}
	}

	dereferenced, err := ctx.Dereference(object)
	if err != nil {
		return fmt.Errorf("%w: object graph failure", ErrInvoicePDFInvalid)
	}
	switch value := dereferenced.(type) {
	case types.Name:
		if value.Value() == "JavaScript" || value.Value() == "Launch" || value.Value() == "EmbeddedFile" {
			return ErrInvoicePDFActiveContent
		}
	case types.Dict:
		return rejectInvoicePDFActiveDict(ctx, value, visited)
	case types.StreamDict:
		return rejectInvoicePDFActiveDict(ctx, value.Dict, visited)
	case types.Array:
		for _, item := range value {
			if err := rejectInvoicePDFActiveGraph(ctx, item, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectInvoicePDFActiveDict(ctx *pdfmodel.Context, dict types.Dict, visited map[string]struct{}) error {
	for key, object := range dict {
		switch key {
		case "OpenAction", "AA", "JavaScript", "EmbeddedFiles", "EF":
			return ErrInvoicePDFActiveContent
		}
		if name, ok := object.(types.Name); ok && (name.Value() == "JavaScript" || name.Value() == "Launch" || name.Value() == "EmbeddedFile") {
			return ErrInvoicePDFActiveContent
		}
		if err := rejectInvoicePDFActiveGraph(ctx, object, visited); err != nil {
			return err
		}
	}
	return nil
}
