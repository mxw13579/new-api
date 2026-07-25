package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
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
	getInput    *s3.GetObjectInput
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
	return &s3.HeadObjectOutput{ContentLength: int64Pointer(4), ChecksumSHA256: stringPointer("checksum"), ETag: stringPointer("\"opaque-head\"")}, s.err
}

func (s *invoiceR2ClientStub) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	s.getInput = input
	return &s3.GetObjectOutput{
		Body: io.NopCloser(strings.NewReader("%PDF")), ContentLength: int64Pointer(4),
		ChecksumSHA256: stringPointer("checksum"), ETag: stringPointer("\"opaque-get\""),
	}, s.err
}

func (s *invoiceR2ClientStub) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	s.deleteInput = input
	return &s3.DeleteObjectOutput{}, s.err
}

func TestInvoiceR2AdapterUsesBoundedPrivateOperations(t *testing.T) {
	client := &invoiceR2ClientStub{}
	authorityID := strings.Repeat("a", 64)
	store, err := NewInvoiceR2Store(client, authorityID, "private-invoices")
	require.NoError(t, err)
	assert.Equal(t, authorityID, store.AuthorityID())
	assert.Equal(t, "private-invoices", store.Bucket())

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
	assert.Equal(t, "\"opaque-head\"", head.ETag)
	assert.Equal(t, types.ChecksumModeEnabled, client.headInput.ChecksumMode)

	object, err := store.Get(context.Background(), "invoices/random.pdf", "\"opaque-head\"")
	require.NoError(t, err)
	defer object.Body.Close()
	assert.Equal(t, "\"opaque-head\"", *client.getInput.IfMatch)
	assert.Equal(t, types.ChecksumModeEnabled, client.getInput.ChecksumMode)
	assert.Equal(t, int64(4), object.SizeBytes)
	assert.Equal(t, "checksum", object.ChecksumSHA256)
	assert.Equal(t, "\"opaque-get\"", object.ETag)
	content, err = io.ReadAll(object.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("%PDF"), content)

	require.NoError(t, store.Delete(context.Background(), "invoices/random.pdf"))
	assert.Equal(t, "invoices/random.pdf", *client.deleteInput.Key)

	assert.ErrorIs(t, store.Put(context.Background(), "tmp/invoices/large.pdf", bytes.NewReader(nil), InvoicePDFMaxBytes+1, "checksum"), ErrInvoiceObjectTerminal)
	_, err = store.Get(context.Background(), "invoices/random.pdf", "")
	assert.ErrorIs(t, err, ErrInvoiceObjectTerminal)
}

func TestInvoiceR2AuthorityIDRequiresExactLowercaseHex(t *testing.T) {
	client := &invoiceR2ClientStub{}
	valid := strings.Repeat("0123456789abcdef", 4)
	tests := []struct {
		name      string
		authority string
		wantError bool
	}{
		{name: "valid", authority: valid},
		{name: "short", authority: valid[:63], wantError: true},
		{name: "uppercase", authority: strings.ToUpper(valid), wantError: true},
		{name: "non hex", authority: strings.Repeat("g", 64), wantError: true},
		{name: "surrounding whitespace", authority: " " + valid, wantError: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store, err := NewInvoiceR2Store(client, testCase.authority, "private-invoices")
			if testCase.wantError {
				assert.ErrorIs(t, err, ErrInvoiceObjectTerminal)
				assert.Nil(t, store)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.authority, store.AuthorityID())
		})
	}
}

func TestInvoiceR2StoreFromSettingDerivesAuthorityFromCloudflareAccount(t *testing.T) {
	lowerAccount := "0123456789abcdef0123456789abcdef"
	upperAccount := strings.ToUpper(lowerAccount)
	expectedDigest := sha256.Sum256([]byte("cloudflare-r2:" + lowerAccount))
	expectedAuthority := hex.EncodeToString(expectedDigest[:])

	setInvoiceR2Setting(t, "https://"+upperAccount+".r2.cloudflarestorage.com:443/", "first-access", "first-secret")
	first, err := NewInvoiceR2StoreFromSetting()
	require.NoError(t, err)
	assert.Equal(t, expectedAuthority, first.AuthorityID())

	setInvoiceR2Setting(t, "https://"+lowerAccount+".r2.cloudflarestorage.com", "rotated-access", "rotated-secret")
	rotated, err := NewInvoiceR2StoreFromSetting()
	require.NoError(t, err)
	assert.Equal(t, first.AuthorityID(), rotated.AuthorityID())

	otherAccount := "1123456789abcdef0123456789abcdef"
	setInvoiceR2Setting(t, "https://"+otherAccount+".r2.cloudflarestorage.com", "rotated-access", "rotated-secret")
	other, err := NewInvoiceR2StoreFromSetting()
	require.NoError(t, err)
	assert.NotEqual(t, first.AuthorityID(), other.AuthorityID())
}

func TestInvoiceR2StoreFromSettingRejectsNonstandardEndpoints(t *testing.T) {
	account := "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name     string
		endpoint string
	}{
		{name: "http", endpoint: "http://" + account + ".r2.cloudflarestorage.com"},
		{name: "userinfo", endpoint: "https://user@" + account + ".r2.cloudflarestorage.com"},
		{name: "nondefault port", endpoint: "https://" + account + ".r2.cloudflarestorage.com:8443"},
		{name: "path", endpoint: "https://" + account + ".r2.cloudflarestorage.com/bucket"},
		{name: "query", endpoint: "https://" + account + ".r2.cloudflarestorage.com?bucket=x"},
		{name: "fragment", endpoint: "https://" + account + ".r2.cloudflarestorage.com#bucket"},
		{name: "wrong host suffix", endpoint: "https://" + account + ".example.com"},
		{name: "extra host label", endpoint: "https://extra." + account + ".r2.cloudflarestorage.com"},
		{name: "short account", endpoint: "https://" + account[:31] + ".r2.cloudflarestorage.com"},
		{name: "nonhex account", endpoint: "https://g123456789abcdef0123456789abcdef.r2.cloudflarestorage.com"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			setInvoiceR2Setting(t, testCase.endpoint, "test-access", "test-secret")
			store, err := NewInvoiceR2StoreFromSetting()
			assert.ErrorIs(t, err, ErrInvoiceR2NotConfigured)
			assert.Nil(t, store)
		})
	}
}

func TestInvoiceR2GetClassifiesPreconditionFailureAsIntegrityUnavailable(t *testing.T) {
	client := &invoiceR2ClientStub{err: &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "etag changed", Fault: smithy.FaultClient}}
	store, err := NewInvoiceR2Store(client, strings.Repeat("a", 64), "private-invoices")
	require.NoError(t, err)

	_, err = store.Get(context.Background(), "invoices/random.pdf", "\"persisted-etag\"")
	assert.ErrorIs(t, err, ErrInvoiceObjectIntegrityUnavailable)
	assert.NotErrorIs(t, err, ErrInvoiceObjectNotFound)
}

func TestInvoiceR2AdapterClassifiesProviderErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider error
		expected error
	}{
		{name: "object not found", provider: &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing", Fault: smithy.FaultClient}, expected: ErrInvoiceObjectNotFound},
		{name: "bucket unavailable", provider: &smithy.GenericAPIError{Code: "NoSuchBucket", Message: "missing bucket", Fault: smithy.FaultClient}, expected: ErrInvoiceObjectBucketUnavailable},
		{name: "ambiguous not found is terminal", provider: &smithy.GenericAPIError{Code: "NotFound", Message: "ambiguous", Fault: smithy.FaultClient}, expected: ErrInvoiceObjectTerminal},
		{name: "unknown client error is terminal", provider: &smithy.GenericAPIError{Code: "InvalidRequest", Message: "bad request", Fault: smithy.FaultClient}, expected: ErrInvoiceObjectTerminal},
		{name: "retryable", provider: &smithy.GenericAPIError{Code: "SlowDown", Message: "later", Fault: smithy.FaultServer}, expected: ErrInvoiceObjectRetryable},
		{name: "access denied is terminal", provider: &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied", Fault: smithy.FaultClient}, expected: ErrInvoiceObjectTerminal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &invoiceR2ClientStub{err: testCase.provider}
			store, err := NewInvoiceR2Store(client, strings.Repeat("a", 64), "private-invoices")
			require.NoError(t, err)
			_, err = store.Head(context.Background(), "invoices/random.pdf")
			require.ErrorIs(t, err, testCase.expected)
			if testCase.expected == ErrInvoiceObjectBucketUnavailable {
				assert.ErrorIs(t, err, ErrInvoiceObjectTerminal)
			}
			assert.NotContains(t, err.Error(), "invoices/random.pdf")
		})
	}

	client := &invoiceR2ClientStub{err: errors.New("transport failed")}
	store, err := NewInvoiceR2Store(client, strings.Repeat("a", 64), "private-invoices")
	require.NoError(t, err)
	_, err = store.Head(context.Background(), "invoices/random.pdf")
	assert.ErrorIs(t, err, ErrInvoiceObjectRetryable)
}

func int64Pointer(value int64) *int64    { return &value }
func stringPointer(value string) *string { return &value }

func setInvoiceR2Setting(t *testing.T, endpoint, accessKeyID, secretAccessKey string) {
	t.Helper()
	previous := operation_setting.GetInvoiceSetting()
	operation_setting.PublishInvoiceSetting(operation_setting.InvoiceSetting{
		ApplicationWindowDays: 30, PDFRetentionDays: 30,
		R2Endpoint: endpoint, R2Bucket: "private-invoices", R2AccessKeyID: accessKeyID, R2Secret: secretAccessKey,
	})
	t.Cleanup(func() { operation_setting.PublishInvoiceSetting(previous) })
}
