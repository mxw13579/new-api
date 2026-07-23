package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// InvoicePDFMaxBytes is the maximum PDF upload size accepted by the invoice document pipeline.
const InvoicePDFMaxBytes int64 = 10 << 20

const (
	invoicePDFMaxDepth = 64
	invoicePDFMaxNodes = 10000
)

var (
	// ErrInvoicePDFTooLarge indicates that an uploaded PDF exceeds InvoicePDFMaxBytes.
	ErrInvoicePDFTooLarge = errors.New("invoice pdf exceeds size limit")
	// ErrInvoicePDFInvalid indicates that uploaded bytes are not a structurally valid PDF.
	ErrInvoicePDFInvalid = errors.New("invoice pdf is invalid")
	// ErrInvoicePDFEncrypted indicates that encrypted PDFs are rejected because their content cannot be safely inspected.
	ErrInvoicePDFEncrypted = errors.New("encrypted invoice pdf is not allowed")
	// ErrInvoicePDFActiveContent indicates that a PDF contains executable or embedded active content.
	ErrInvoicePDFActiveContent = errors.New("active invoice pdf content is not allowed")
)

// InvoicePDFValidation contains the bounded size and SHA-256 identity of a structurally safe PDF.
type InvoicePDFValidation struct {
	SizeBytes int64
	SHA256    string
}

// ValidateInvoicePDF parses bounded PDF bytes and rejects encryption, active actions, and embedded content.
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
	root, err := ctx.Catalog()
	if err != nil {
		return InvoicePDFValidation{}, fmt.Errorf("%w: catalog unavailable", ErrInvoicePDFInvalid)
	}
	if err := rejectInvoicePDFActiveGraph(ctx, root); err != nil {
		return InvoicePDFValidation{}, err
	}
	if err := api.ValidateContext(ctx); err != nil {
		return InvoicePDFValidation{}, fmt.Errorf("%w: structural validation failed", ErrInvoicePDFInvalid)
	}

	digest := sha256.Sum256(data)
	return InvoicePDFValidation{SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}, nil
}

type invoicePDFTraversal struct {
	ctx      *pdfmodel.Context
	nodes    int
	indirect map[string]struct{}
	compound map[uintptr]struct{}
}

func rejectInvoicePDFActiveGraph(ctx *pdfmodel.Context, object types.Object) error {
	traversal := invoicePDFTraversal{ctx: ctx, indirect: map[string]struct{}{}, compound: map[uintptr]struct{}{}}
	return traversal.walk(object, 0, false, invoicePDFLocationCatalog)
}

type invoicePDFLocation uint8

const (
	invoicePDFLocationCatalog invoicePDFLocation = iota
	invoicePDFLocationGeneric
	invoicePDFLocationAction
	invoicePDFLocationActionMap
	invoicePDFLocationNames
	invoicePDFLocationOutlineRoot
	invoicePDFLocationOutlineItem
)

func (traversal *invoicePDFTraversal) walk(object types.Object, depth int, count bool, location invoicePDFLocation) error {
	if depth > invoicePDFMaxDepth {
		return fmt.Errorf("%w: object graph depth exceeded", ErrInvoicePDFInvalid)
	}
	if count {
		traversal.nodes++
		if traversal.nodes > invoicePDFMaxNodes {
			return fmt.Errorf("%w: object graph node budget exceeded", ErrInvoicePDFInvalid)
		}
	}
	if reference, ok := object.(types.IndirectRef); ok {
		key := reference.PDFString()
		if _, seen := traversal.indirect[key]; seen {
			return nil
		}
		traversal.indirect[key] = struct{}{}
		traversal.nodes++
		if traversal.nodes > invoicePDFMaxNodes || traversal.ctx == nil {
			return fmt.Errorf("%w: object graph dereference failed", ErrInvoicePDFInvalid)
		}
	}
	dereferenced := object
	if traversal.ctx != nil {
		var err error
		dereferenced, err = traversal.ctx.Dereference(object)
		if err != nil {
			return fmt.Errorf("%w: object graph failure", ErrInvoicePDFInvalid)
		}
	}
	switch value := dereferenced.(type) {
	case types.Dict:
		if traversal.seenCompound(value) {
			return nil
		}
		return traversal.walkDict(value, depth, location)
	case types.StreamDict:
		if traversal.seenCompound(value.Dict) {
			return nil
		}
		return traversal.walkDict(value.Dict, depth, location)
	case types.Array:
		if traversal.seenCompound(value) {
			return nil
		}
		for _, item := range value {
			if err := traversal.walk(item, depth+1, true, location); err != nil {
				return err
			}
		}
	}
	return nil
}

func (traversal *invoicePDFTraversal) seenCompound(value any) bool {
	pointer := reflect.ValueOf(value).Pointer()
	if pointer == 0 {
		return false
	}
	if _, seen := traversal.compound[pointer]; seen {
		return true
	}
	traversal.compound[pointer] = struct{}{}
	return false
}

func (traversal *invoicePDFTraversal) walkDict(dict types.Dict, depth int, location invoicePDFLocation) error {
	if location == invoicePDFLocationAction {
		action, err := traversal.policyName(dict["S"])
		if err != nil {
			return err
		}
		if dangerousInvoicePDFAction(action) {
			return ErrInvoicePDFActiveContent
		}
	}
	if location == invoicePDFLocationNames {
		if _, ok := dict["JavaScript"]; ok {
			return ErrInvoicePDFActiveContent
		}
		if _, ok := dict["EmbeddedFiles"]; ok {
			return ErrInvoicePDFActiveContent
		}
	}
	typeName, err := traversal.policyName(dict["Type"])
	if err != nil {
		return err
	}
	if typeName == "Filespec" {
		if _, ok := dict["EF"]; ok {
			return ErrInvoicePDFActiveContent
		}
	}
	subtypeName, err := traversal.policyName(dict["Subtype"])
	if err != nil {
		return err
	}
	legalAdditionalActions := location == invoicePDFLocationCatalog || typeName == "Page" || typeName == "Annot" || subtypeName == "Widget"
	if _, field := dict["FT"]; field {
		legalAdditionalActions = true
	}
	for key, child := range dict {
		next := invoicePDFLocationGeneric
		switch {
		case location == invoicePDFLocationAction && key == "Next":
			next = invoicePDFLocationAction
		case location == invoicePDFLocationActionMap:
			next = invoicePDFLocationAction
		case location == invoicePDFLocationCatalog && key == "OpenAction":
			next = invoicePDFLocationAction
		case legalAdditionalActions && key == "AA":
			next = invoicePDFLocationActionMap
		case location == invoicePDFLocationCatalog && key == "Names":
			next = invoicePDFLocationNames
		case location == invoicePDFLocationCatalog && key == "Outlines":
			next = invoicePDFLocationOutlineRoot
		case location == invoicePDFLocationOutlineRoot && (key == "First" || key == "Last"):
			next = invoicePDFLocationOutlineItem
		case location == invoicePDFLocationOutlineItem && key == "A":
			next = invoicePDFLocationAction
		case location == invoicePDFLocationOutlineItem && (key == "First" || key == "Last" || key == "Next" || key == "Prev"):
			next = invoicePDFLocationOutlineItem
		case (subtypeName == "Link" || subtypeName == "Widget" || subtypeName == "Screen") && key == "A":
			next = invoicePDFLocationAction
		case subtypeName == "Link" && key == "PA":
			next = invoicePDFLocationAction
		}
		if err := traversal.walk(child, depth+1, true, next); err != nil {
			return err
		}
	}
	return nil
}

func (traversal *invoicePDFTraversal) policyName(object types.Object) (string, error) {
	if name, ok := object.(types.Name); ok {
		return name.Value(), nil
	}
	reference, ok := object.(types.IndirectRef)
	if !ok {
		return "", nil
	}
	key := reference.PDFString()
	if _, seen := traversal.indirect[key]; !seen {
		traversal.indirect[key] = struct{}{}
		traversal.nodes++
		if traversal.nodes > invoicePDFMaxNodes {
			return "", fmt.Errorf("%w: object graph node budget exceeded", ErrInvoicePDFInvalid)
		}
	}
	if traversal.ctx == nil {
		return "", fmt.Errorf("%w: object graph dereference failed", ErrInvoicePDFInvalid)
	}
	dereferenced, err := traversal.ctx.Dereference(reference)
	if err != nil {
		return "", fmt.Errorf("%w: object graph failure", ErrInvoicePDFInvalid)
	}
	name, _ := dereferenced.(types.Name)
	return name.Value(), nil
}

func dangerousInvoicePDFAction(action string) bool {
	switch action {
	case "JavaScript", "Launch", "URI", "GoToR", "GoToE", "SubmitForm", "ImportData", "Rendition", "RichMediaExecute":
		return true
	default:
		return false
	}
}
