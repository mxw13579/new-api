package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecompressRequestMiddlewareRejectsZstdWindowAboveBodyLimit(t *testing.T) {
	originalMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 32
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = originalMaxRequestBodyMB
	})

	// This complete frame header declares a 512 MiB window while the entire
	// request body is only six bytes. The decoder must reject it at the header.
	hostileFrame := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x98}
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(hostileFrame))
	request.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()

	var readErr error
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/", func(c *gin.Context) {
		_, readErr = io.ReadAll(c.Request.Body)
		if readErr != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	router.ServeHTTP(recorder, request)
	require.Error(t, readErr)
	assert.ErrorIs(t, readErr, zstd.ErrWindowSizeExceeded)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestDecompressRequestMiddlewareDecodesZstdBody(t *testing.T) {
	originalMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 32
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = originalMaxRequestBodyMB
	})

	writer, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressed := writer.EncodeAll([]byte(`{"message":"ok"}`), nil)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed))
	request.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()

	var body []byte
	var readErr error
	var contentEncoding string
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/", func(c *gin.Context) {
		body, readErr = io.ReadAll(c.Request.Body)
		contentEncoding = c.GetHeader("Content-Encoding")
		c.Status(http.StatusNoContent)
	})

	router.ServeHTTP(recorder, request)
	require.NoError(t, readErr)
	assert.Equal(t, []byte(`{"message":"ok"}`), body)
	assert.Empty(t, contentEncoding)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestDecompressRequestMiddlewareKeepsGzipBehavior(t *testing.T) {
	originalMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 32
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = originalMaxRequestBodyMB
	})

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(`{"message":"ok"}`))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()

	var body []byte
	var readErr error
	var contentEncoding string
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/", func(c *gin.Context) {
		body, readErr = io.ReadAll(c.Request.Body)
		contentEncoding = c.GetHeader("Content-Encoding")
		c.Status(http.StatusNoContent)
	})

	router.ServeHTTP(recorder, request)
	require.NoError(t, readErr)
	assert.Equal(t, []byte(`{"message":"ok"}`), body)
	assert.Empty(t, contentEncoding)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}
