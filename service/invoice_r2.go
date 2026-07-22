package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// InvoicePDFContentType is the fixed media type used for all persisted and downloaded invoice PDFs.
const InvoicePDFContentType = model.InvoicePDFContentType

var (
	// ErrInvoiceObjectNotFound classifies a trusted object-store response that proves the requested key is absent.
	ErrInvoiceObjectNotFound = errors.New("invoice object not found")
	// ErrInvoiceObjectRetryable classifies transient object-store or transport failures.
	ErrInvoiceObjectRetryable = errors.New("invoice object operation retryable")
	// ErrInvoiceObjectTerminal classifies invalid configuration, unsafe inputs, or non-retryable provider failures.
	ErrInvoiceObjectTerminal = errors.New("invoice object operation terminal")
)

type invoiceR2Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type invoiceR2Presigner interface {
	PresignGetObject(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// InvoiceObjectHead contains the durable size and checksum facts used to verify a promoted PDF object.
type InvoiceObjectHead struct {
	SizeBytes      int64
	ChecksumSHA256 string
}

// InvoiceR2Store implements the private invoice object contract for one trusted R2 bucket.
type InvoiceR2Store struct {
	client    invoiceR2Client
	presigner invoiceR2Presigner
	bucket    string
}

// NewInvoiceR2Store validates dependencies and creates a store restricted to the supplied private bucket.
func NewInvoiceR2Store(client invoiceR2Client, presigner invoiceR2Presigner, bucket string) (*InvoiceR2Store, error) {
	if client == nil || presigner == nil || strings.TrimSpace(bucket) == "" {
		return nil, ErrInvoiceObjectTerminal
	}
	return &InvoiceR2Store{client: client, presigner: presigner, bucket: bucket}, nil
}

// NewInvoiceR2StoreFromEnvironment creates the trusted invoice store from validated HTTPS R2 configuration.
func NewInvoiceR2StoreFromEnvironment() (*InvoiceR2Store, error) {
	endpoint := strings.TrimSpace(os.Getenv("INVOICE_R2_ENDPOINT"))
	bucket := strings.TrimSpace(os.Getenv("INVOICE_R2_BUCKET"))
	accessKeyID := strings.TrimSpace(os.Getenv("INVOICE_R2_ACCESS_KEY_ID"))
	secretAccessKey := strings.TrimSpace(os.Getenv("INVOICE_R2_SECRET_ACCESS_KEY"))
	if endpoint == "" || bucket == "" || accessKeyID == "" || secretAccessKey == "" {
		return nil, ErrInvoiceObjectTerminal
	}
	if parsed, err := url.Parse(endpoint); err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, ErrInvoiceObjectTerminal
	}
	config := aws.Config{
		Region: "auto", BaseEndpoint: aws.String(strings.TrimRight(endpoint, "/")),
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	}
	client := s3.NewFromConfig(config)
	return NewInvoiceR2Store(client, s3.NewPresignClient(client), bucket)
}

// Bucket returns the trusted R2 bucket bound to this store instance.
func (s *InvoiceR2Store) Bucket() string {
	if s == nil {
		return ""
	}
	return s.bucket
}

func invoiceObjectStoreMatchesBucket(store InvoiceObjectStore, persistedBucket string) bool {
	bucketStore, ok := store.(interface{ Bucket() string })
	return ok && strings.TrimSpace(persistedBucket) != "" && bucketStore.Bucket() == persistedBucket
}

// Put streams a bounded PDF staging object with its expected SHA-256 checksum.
func (s *InvoiceR2Store) Put(ctx context.Context, key string, body io.Reader, size int64, checksumSHA256 string) error {
	if err := validateInvoiceObjectKey(key); err != nil || body == nil || size <= 0 || size > InvoicePDFMaxBytes {
		return ErrInvoiceObjectTerminal
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: body,
		ContentLength: aws.Int64(size), ContentType: aws.String(model.InvoicePDFContentType),
		ChecksumSHA256: aws.String(checksumSHA256),
	})
	return classifyInvoiceObjectError(err)
}

// Copy promotes a persisted staging key to a persisted final key within the trusted bucket.
func (s *InvoiceR2Store) Copy(ctx context.Context, sourceKey, destinationKey string) error {
	if validateInvoiceObjectKey(sourceKey) != nil || validateInvoiceObjectKey(destinationKey) != nil {
		return ErrInvoiceObjectTerminal
	}
	copySource := url.PathEscape(s.bucket + "/" + sourceKey)
	copySource = strings.ReplaceAll(copySource, "%2F", "/")
	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(destinationKey), CopySource: aws.String(copySource),
		ContentType: aws.String(model.InvoicePDFContentType),
	})
	return classifyInvoiceObjectError(err)
}

// Head returns the stored size and checksum used to prove final-object integrity.
func (s *InvoiceR2Store) Head(ctx context.Context, key string) (InvoiceObjectHead, error) {
	if validateInvoiceObjectKey(key) != nil {
		return InvoiceObjectHead{}, ErrInvoiceObjectTerminal
	}
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return InvoiceObjectHead{}, classifyInvoiceObjectError(err)
	}
	return InvoiceObjectHead{SizeBytes: aws.ToInt64(output.ContentLength), ChecksumSHA256: aws.ToString(output.ChecksumSHA256)}, nil
}

// Delete removes a validated persisted key from the trusted private bucket.
func (s *InvoiceR2Store) Delete(ctx context.Context, key string) error {
	if validateInvoiceObjectKey(key) != nil {
		return ErrInvoiceObjectTerminal
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return classifyInvoiceObjectError(err)
}

// PresignGet creates a bounded attachment URL for a validated final invoice key.
func (s *InvoiceR2Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if validateInvoiceObjectKey(key) != nil || ttl <= 0 || ttl > 5*time.Minute {
		return "", ErrInvoiceObjectTerminal
	}
	request, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key),
		ResponseContentType:        aws.String(model.InvoicePDFContentType),
		ResponseContentDisposition: aws.String("attachment; filename=invoice.pdf"),
	}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if err != nil {
		return "", classifyInvoiceObjectError(err)
	}
	return request.URL, nil
}

func validateInvoiceObjectKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") || strings.ContainsAny(key, "\r\n") {
		return ErrInvoiceObjectTerminal
	}
	if !strings.HasPrefix(key, "tmp/invoices/") && !strings.HasPrefix(key, "invoices/") {
		return ErrInvoiceObjectTerminal
	}
	return nil
}

func classifyInvoiceObjectError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: canceled", ErrInvoiceObjectTerminal)
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket":
			return fmt.Errorf("%w: provider not found", ErrInvoiceObjectNotFound)
		case "SlowDown", "RequestTimeout", "InternalError", "ServiceUnavailable", "Throttling", "ThrottlingException":
			return fmt.Errorf("%w: provider retryable", ErrInvoiceObjectRetryable)
		default:
			if apiError.ErrorFault() == smithy.FaultServer {
				return fmt.Errorf("%w: provider server fault", ErrInvoiceObjectRetryable)
			}
			return fmt.Errorf("%w: provider rejected request", ErrInvoiceObjectTerminal)
		}
	}
	return fmt.Errorf("%w: transport failure", ErrInvoiceObjectRetryable)
}
