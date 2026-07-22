package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type invoiceR2ClientStub struct {
	putInput    *s3.PutObjectInput
	copyInput   *s3.CopyObjectInput
	headInput   *s3.HeadObjectInput
	deleteInput *s3.DeleteObjectInput
	err         error
}

func (s *invoiceR2ClientStub) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	s.putInput = input
	return &s3.PutObjectOutput{}, s.err
}

func (s *invoiceR2ClientStub) CopyObject(_ context.Context, input *s3.CopyObjectInput, _ ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	s.copyInput = input
	return &s3.CopyObjectOutput{}, s.err
}

func (s *invoiceR2ClientStub) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	s.headInput = input
	return &s3.HeadObjectOutput{ContentLength: int64Pointer(4), ChecksumSHA256: stringPointer("checksum")}, s.err
}

func (s *invoiceR2ClientStub) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	s.deleteInput = input
	return &s3.DeleteObjectOutput{}, s.err
}

type invoiceR2PresignerStub struct {
	input *s3.GetObjectInput
	ttl   time.Duration
	err   error
}

func (s *invoiceR2PresignerStub) PresignGetObject(_ context.Context, input *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	s.input = input
	options := s3.PresignOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	s.ttl = options.Expires
	return &v4.PresignedHTTPRequest{URL: "https://private.invalid/signed", Method: "GET", SignedHeader: map[string][]string{}}, s.err
}

func TestInvoiceR2AdapterUsesBoundedPrivateOperations(t *testing.T) {
	client := &invoiceR2ClientStub{}
	presigner := &invoiceR2PresignerStub{}
	store, err := NewInvoiceR2Store(client, presigner, "private-invoices")
	require.NoError(t, err)

	err = store.Put(context.Background(), "tmp/invoices/random.pdf", bytes.NewReader([]byte("%PDF")), 4, "checksum")
	require.NoError(t, err)
	require.NotNil(t, client.putInput)
	assert.Equal(t, "private-invoices", *client.putInput.Bucket)
	assert.Equal(t, "tmp/invoices/random.pdf", *client.putInput.Key)
	assert.Equal(t, InvoicePDFContentType, *client.putInput.ContentType)
	assert.Equal(t, int64(4), *client.putInput.ContentLength)
	assert.Empty(t, client.putInput.ACL)
	content, err := io.ReadAll(client.putInput.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("%PDF"), content)

	require.NoError(t, store.Copy(context.Background(), "tmp/invoices/random.pdf", "invoices/random.pdf"))
	assert.Equal(t, "private-invoices/tmp/invoices/random.pdf", *client.copyInput.CopySource)
	assert.Equal(t, "invoices/random.pdf", *client.copyInput.Key)

	head, err := store.Head(context.Background(), "invoices/random.pdf")
	require.NoError(t, err)
	assert.Equal(t, int64(4), head.SizeBytes)
	assert.Equal(t, "checksum", head.ChecksumSHA256)
	assert.Equal(t, types.ChecksumModeEnabled, client.headInput.ChecksumMode)

	url, err := store.PresignGet(context.Background(), "invoices/random.pdf", 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "https://private.invalid/signed", url)
	assert.Equal(t, 5*time.Minute, presigner.ttl)
	assert.Equal(t, "attachment; filename=invoice.pdf", *presigner.input.ResponseContentDisposition)

	require.NoError(t, store.Delete(context.Background(), "invoices/random.pdf"))
	assert.Equal(t, "invoices/random.pdf", *client.deleteInput.Key)

	assert.ErrorIs(t, store.Put(context.Background(), "tmp/invoices/large.pdf", bytes.NewReader(nil), InvoicePDFMaxBytes+1, "checksum"), ErrInvoiceObjectTerminal)
	_, err = store.PresignGet(context.Background(), "invoices/random.pdf", 5*time.Minute+time.Second)
	assert.ErrorIs(t, err, ErrInvoiceObjectTerminal)
}

func TestInvoiceR2AdapterClassifiesProviderErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider error
		expected error
	}{
		{name: "not found", provider: &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing", Fault: smithy.FaultClient}, expected: ErrInvoiceObjectNotFound},
		{name: "retryable", provider: &smithy.GenericAPIError{Code: "SlowDown", Message: "later", Fault: smithy.FaultServer}, expected: ErrInvoiceObjectRetryable},
		{name: "terminal", provider: &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied", Fault: smithy.FaultClient}, expected: ErrInvoiceObjectTerminal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &invoiceR2ClientStub{err: testCase.provider}
			store, err := NewInvoiceR2Store(client, &invoiceR2PresignerStub{}, "private-invoices")
			require.NoError(t, err)
			_, err = store.Head(context.Background(), "invoices/random.pdf")
			require.ErrorIs(t, err, testCase.expected)
			assert.NotContains(t, err.Error(), "invoices/random.pdf")
		})
	}

	client := &invoiceR2ClientStub{err: errors.New("transport failed")}
	store, err := NewInvoiceR2Store(client, &invoiceR2PresignerStub{}, "private-invoices")
	require.NoError(t, err)
	_, err = store.Head(context.Background(), "invoices/random.pdf")
	assert.ErrorIs(t, err, ErrInvoiceObjectRetryable)
}

func int64Pointer(value int64) *int64    { return &value }
func stringPointer(value string) *string { return &value }
