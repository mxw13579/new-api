package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingFrontendFS struct {
	static.ServeFileSystem
	opens atomic.Int32
}

func (f *countingFrontendFS) Open(name string) (http.File, error) {
	f.opens.Add(1)
	return f.ServeFileSystem.Open(name)
}

func performFrontendRequest(router http.Handler, method, target, acceptEncoding string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func newFrontendTestRouter(t *testing.T) (http.Handler, *countingFrontendFS, []byte, []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	frontendDir := t.TempDir()
	script := []byte(strings.Repeat("export const answer = 42;\n", 400))
	image := []byte("\x89PNG\r\n\x1a\nnot-really-an-image")
	require.NoError(t, os.MkdirAll(filepath.Join(frontendDir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.js"), script, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "logo.png"), image, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html></html>"), 0o644))
	frontendFS := &countingFrontendFS{ServeFileSystem: static.LocalFile(frontendDir, false)}

	router := gin.New()
	router.NoRoute(ServeFrontendFiles(frontendFS), func(c *gin.Context) {
		c.String(http.StatusTeapot, "fallback")
	})
	return router, frontendFS, script, image
}

func decodeFrontendBody(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	var reader io.Reader = bytes.NewReader(body)
	switch encoding {
	case "br":
		reader = brotli.NewReader(reader)
	case "gzip":
		gzipReader, err := gzip.NewReader(reader)
		require.NoError(t, err)
		reader = gzipReader
	}
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	return decoded
}

func TestServeFrontendFilesCompressesEachFileOnce(t *testing.T) {
	router, frontendFS, script, image := newFrontendTestRouter(t)

	plain := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "")
	require.Equal(t, http.StatusOK, plain.Code)
	assert.Empty(t, plain.Header().Get("Content-Encoding"))
	assert.Equal(t, script, plain.Body.Bytes())
	frontendFS.opens.Store(0)

	// Each encoding is compressed lazily on its own first requests: gzip
	// first, then brotli, with concurrent first requests sharing one result.
	for i, encoding := range []string{"gzip", "br"} {
		responses := make([]*httptest.ResponseRecorder, 8)
		var wg sync.WaitGroup
		for j := range responses {
			wg.Go(func() {
				responses[j] = performFrontendRequest(router, http.MethodGet, "/static/js/app.js", encoding+", deflate")
			})
		}
		wg.Wait()
		for _, response := range responses {
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, encoding, response.Header().Get("Content-Encoding"))
			assert.Equal(t, "Accept-Encoding", response.Header().Get("Vary"))
			assert.Equal(t, plain.Header().Get("Content-Type"), response.Header().Get("Content-Type"))
			assert.Equal(t, strconv.Itoa(response.Body.Len()), response.Header().Get("Content-Length"))
			assert.Equal(t, responses[0].Body.Bytes(), response.Body.Bytes())
		}
		assert.Equal(t, script, decodeFrontendBody(t, encoding, responses[0].Body.Bytes()))

		head := performFrontendRequest(router, http.MethodHead, "/static/js/app.js", encoding)
		assert.Equal(t, http.StatusOK, head.Code)
		assert.Equal(t, encoding, head.Header().Get("Content-Encoding"))
		assert.Equal(t, strconv.Itoa(responses[0].Body.Len()), head.Header().Get("Content-Length"))
		assert.Empty(t, head.Body.Bytes())
		assert.Equal(t, int32(i+1), frontendFS.opens.Load(), "%s must be read and compressed once, on its first request", encoding)
	}

	gzipAfterBrotli := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "gzip")
	require.Equal(t, http.StatusOK, gzipAfterBrotli.Code)
	assert.Equal(t, "gzip", gzipAfterBrotli.Header().Get("Content-Encoding"))
	assert.Equal(t, script, decodeFrontendBody(t, "gzip", gzipAfterBrotli.Body.Bytes()))
	assert.Equal(t, int32(2), frontendFS.opens.Load(), "cached gzip must be reused after brotli is cached")

	png := performFrontendRequest(router, http.MethodGet, "/logo.png", "gzip")
	assert.Equal(t, http.StatusOK, png.Code)
	assert.Empty(t, png.Header().Get("Content-Encoding"))
	assert.Equal(t, image, png.Body.Bytes())

	index := performFrontendRequest(router, http.MethodGet, "/index.html", "gzip")
	assert.Equal(t, http.StatusMovedPermanently, index.Code)
	assert.Equal(t, "./", index.Header().Get("Location"))

	missing := performFrontendRequest(router, http.MethodGet, "/static/js/missing.js", "gzip")
	assert.Equal(t, http.StatusTeapot, missing.Code)
	assert.Equal(t, "fallback", missing.Body.String())
}

func TestServeFrontendFilesNegotiatesEncoding(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		target         string
		acceptEncoding string
		wantEncoding   string
		wantBodyFile   string
	}{
		{name: "brotli only", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "br", wantEncoding: "br", wantBodyFile: "script"},
		{name: "gzip only", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "gzip", wantEncoding: "gzip", wantBodyFile: "script"},
		{name: "browser header prefers brotli", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "gzip, deflate, br, zstd", wantEncoding: "br", wantBodyFile: "script"},
		{name: "brotli refused with q=0", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "br;q=0, gzip", wantEncoding: "gzip", wantBodyFile: "script"},
		{name: "spaced q=0.0 refuses brotli", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "gzip;q=0.5, BR ; q=0.0", wantEncoding: "gzip", wantBodyFile: "script"},
		{name: "both refused", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "br;q=0, gzip;q=0", wantEncoding: "", wantBodyFile: "script"},
		{name: "neither accepted", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "deflate, zstd", wantEncoding: "", wantBodyFile: "script"},
		{name: "substrings are not tokens", method: http.MethodGet, target: "/static/js/app.js", acceptEncoding: "x-gzip-like, brotli", wantEncoding: "", wantBodyFile: "script"},
		{name: "excluded extension stays uncompressed", method: http.MethodGet, target: "/logo.png", acceptEncoding: "gzip, br", wantEncoding: "", wantBodyFile: "image"},
		{name: "head with brotli", method: http.MethodHead, target: "/static/js/app.js", acceptEncoding: "br", wantEncoding: "br", wantBodyFile: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _, script, image := newFrontendTestRouter(t)
			files := map[string][]byte{"script": script, "image": image}
			response := performFrontendRequest(router, tt.method, tt.target, tt.acceptEncoding)

			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, tt.wantEncoding, response.Header().Get("Content-Encoding"))
			if tt.wantEncoding != "" {
				assert.Equal(t, "Accept-Encoding", response.Header().Get("Vary"))
				assert.Equal(t, "text/javascript; charset=utf-8", response.Header().Get("Content-Type"))
			}
			if tt.method == http.MethodHead {
				assert.Empty(t, response.Body.Bytes())
				get := performFrontendRequest(router, http.MethodGet, tt.target, tt.acceptEncoding)
				assert.Equal(t, strconv.Itoa(get.Body.Len()), response.Header().Get("Content-Length"))
				return
			}
			assert.Equal(t, strconv.Itoa(response.Body.Len()), response.Header().Get("Content-Length"))
			assert.Equal(t, files[tt.wantBodyFile], decodeFrontendBody(t, tt.wantEncoding, response.Body.Bytes()))
		})
	}
}
