package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// InvoicePDFContentType is the fixed media type used for all persisted and downloaded invoice PDFs.
const InvoicePDFContentType = model.InvoicePDFContentType

const invoiceR2EndpointSuffix = ".r2.cloudflarestorage.com"

var (
	// ErrInvoiceObjectNotFound classifies a trusted object-store response that proves the requested key is absent.
	ErrInvoiceObjectNotFound = errors.New("invoice object not found")
	// ErrInvoiceObjectRetryable classifies transient object-store or transport failures.
	ErrInvoiceObjectRetryable = errors.New("invoice object operation retryable")
	// ErrInvoiceObjectTerminal classifies invalid configuration, unsafe inputs, or non-retryable provider failures.
	ErrInvoiceObjectTerminal = errors.New("invoice object operation terminal")
	// ErrInvoiceR2NotConfigured identifies missing or invalid database-backed invoice storage settings.
	ErrInvoiceR2NotConfigured = errors.New("invoice R2 storage is not configured or invalid")
	// ErrInvoiceObjectIntegrityUnavailable classifies a failed immutable conditional read.
	ErrInvoiceObjectIntegrityUnavailable = fmt.Errorf("%w: invoice object integrity unavailable", ErrInvoiceObjectTerminal)
	// ErrInvoiceObjectBucketUnavailable classifies a terminal response that does not prove an individual object is absent.
	ErrInvoiceObjectBucketUnavailable = fmt.Errorf("%w: invoice object bucket unavailable", ErrInvoiceObjectTerminal)
)

type invoiceR2Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// InvoiceObjectHead contains the durable size and checksum facts used to verify a promoted PDF object.
type InvoiceObjectHead struct {
	SizeBytes      int64
	ChecksumSHA256 string
	ETag           string
}

// InvoiceObjectGet contains a conditional object's stream and immutable provider facts.
type InvoiceObjectGet struct {
	Body           io.ReadCloser
	SizeBytes      int64
	ChecksumSHA256 string
	ETag           string
}

// InvoiceR2Store implements the private invoice object contract for one trusted R2 bucket.
type InvoiceR2Store struct {
	client    invoiceR2Client
	authority string
	bucket    string
}

// NewInvoiceR2Store validates dependencies and creates a store restricted to the supplied private bucket.
func NewInvoiceR2Store(client invoiceR2Client, authority, bucket string) (*InvoiceR2Store, error) {
	if client == nil || !validInvoiceR2AuthorityID(authority) || strings.TrimSpace(bucket) == "" {
		return nil, ErrInvoiceObjectTerminal
	}
	return &InvoiceR2Store{client: client, authority: authority, bucket: bucket}, nil
}

// ValidateInvoiceR2Configuration validates the complete database-backed R2 credential set without making a network request.
func ValidateInvoiceR2Configuration(endpoint, bucket, accessKeyID, secretAccessKey string) error {
	endpoint = strings.TrimSpace(endpoint)
	bucket = strings.TrimSpace(bucket)
	accessKeyID = strings.TrimSpace(accessKeyID)
	secretAccessKey = strings.TrimSpace(secretAccessKey)
	if endpoint == "" || bucket == "" || accessKeyID == "" || secretAccessKey == "" {
		return ErrInvoiceR2NotConfigured
	}
	_, _, ok := invoiceR2EndpointAuthority(endpoint)
	if !ok {
		return ErrInvoiceR2NotConfigured
	}
	return nil
}

// NewInvoiceR2StoreFromSetting creates the trusted invoice store exclusively from persisted invoice settings.
func NewInvoiceR2StoreFromSetting() (*InvoiceR2Store, error) {
	setting := operation_setting.GetInvoiceSetting()
	return newInvoiceR2StoreFromSetting(setting)
}

func newInvoiceR2StoreFromSetting(setting operation_setting.InvoiceSetting) (*InvoiceR2Store, error) {
	endpoint := strings.TrimSpace(setting.R2Endpoint)
	bucket := strings.TrimSpace(setting.R2Bucket)
	accessKeyID := strings.TrimSpace(setting.R2AccessKeyID)
	secretAccessKey := strings.TrimSpace(setting.R2Secret)
	if err := ValidateInvoiceR2Configuration(endpoint, bucket, accessKeyID, secretAccessKey); err != nil {
		return nil, err
	}
	normalizedEndpoint, authority, _ := invoiceR2EndpointAuthority(endpoint)
	config := aws.Config{
		Region: "auto", BaseEndpoint: aws.String(normalizedEndpoint),
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	}
	client := s3.NewFromConfig(config)
	return NewInvoiceR2Store(client, authority, bucket)
}

func invoiceR2EndpointAuthority(endpoint string) (string, string, bool) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", "", false
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", "", false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if len(hostname) != 32+len(invoiceR2EndpointSuffix) || !strings.HasSuffix(hostname, invoiceR2EndpointSuffix) {
		return "", "", false
	}
	accountID := strings.TrimSuffix(hostname, invoiceR2EndpointSuffix)
	if !validInvoiceR2AccountID(accountID) {
		return "", "", false
	}
	digest := sha256.Sum256([]byte("cloudflare-r2:" + accountID))
	return "https://" + hostname, hex.EncodeToString(digest[:]), true
}

// AuthorityID returns the stable non-secret identity bound to this store instance.
func (s *InvoiceR2Store) AuthorityID() string {
	if s == nil {
		return ""
	}
	return s.authority
}

// Bucket returns the trusted R2 bucket bound to this store instance.
func (s *InvoiceR2Store) Bucket() string {
	if s == nil {
		return ""
	}
	return s.bucket
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
	return InvoiceObjectHead{
		SizeBytes: aws.ToInt64(output.ContentLength), ChecksumSHA256: aws.ToString(output.ChecksumSHA256), ETag: aws.ToString(output.ETag),
	}, nil
}

// Get conditionally streams an object only while its opaque provider ETag still matches.
func (s *InvoiceR2Store) Get(ctx context.Context, key, ifMatch string) (InvoiceObjectGet, error) {
	if validateInvoiceObjectKey(key) != nil || strings.TrimSpace(ifMatch) == "" {
		return InvoiceObjectGet{}, ErrInvoiceObjectTerminal
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), IfMatch: aws.String(ifMatch), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return InvoiceObjectGet{}, classifyInvoiceObjectError(err)
	}
	return InvoiceObjectGet{
		Body: output.Body, SizeBytes: aws.ToInt64(output.ContentLength),
		ChecksumSHA256: aws.ToString(output.ChecksumSHA256), ETag: aws.ToString(output.ETag),
	}, nil
}

// Delete removes a validated persisted key from the trusted private bucket.
func (s *InvoiceR2Store) Delete(ctx context.Context, key string) error {
	if validateInvoiceObjectKey(key) != nil {
		return ErrInvoiceObjectTerminal
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return classifyInvoiceObjectError(err)
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
		case "NoSuchKey":
			return fmt.Errorf("%w: provider not found", ErrInvoiceObjectNotFound)
		case "NoSuchBucket":
			return fmt.Errorf("%w: provider bucket unavailable", ErrInvoiceObjectBucketUnavailable)
		case "PreconditionFailed":
			return fmt.Errorf("%w: provider precondition failed", ErrInvoiceObjectIntegrityUnavailable)
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

func validInvoiceR2AuthorityID(authority string) bool {
	return validInvoiceR2LowerHex(authority, 64)
}

func validInvoiceR2AccountID(accountID string) bool {
	return validInvoiceR2LowerHex(accountID, 32)
}

func validInvoiceR2LowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
