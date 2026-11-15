package middleware

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	ginGzip "github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// frontendBrotliLevel balances size against the one-time cost of the first
// request per file. Across the whole build, level 6 output is about 1.5%
// larger than level 9 (up to about 8% on a locale chunk), while each level 9
// compression allocates about 32 MB against about 2 MB here and
// takes longer; after a release, dozens of distinct files compressed
// concurrently would otherwise push the heap toward a gigabyte. Levels 10 and
// 11 take several seconds on the largest lazy chunk.
const frontendBrotliLevel = 6

// precompressedFileKey identifies one content encoding of one build file.
type precompressedFileKey struct {
	urlPath  string
	encoding string
}

// precompressedFile is one content encoding of one frontend build file.
type precompressedFile struct {
	once        sync.Once
	body        []byte
	contentType string
	err         error
}

// ServeFrontendFiles serves files from the embedded frontend build. The build
// never changes while the process runs, so each file is compressed once per
// content encoding, on the first request that asks for that encoding, and the
// bytes are reused instead of recompressing multi-megabyte chunks on every
// request. Brotli is preferred over gzip when the client accepts both. The
// cache holds at most one entry per build file and encoding. Clients that
// accept neither encoding, the image types the gzip middleware skips,
// directories, and the index.html redirect keep using http.FileServer.
func ServeFrontendFiles(frontendFS static.ServeFileSystem) gin.HandlerFunc {
	fileServer := http.FileServer(frontendFS)
	var cache sync.Map // precompressedFileKey -> *precompressedFile
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		if !frontendFS.Exists("/", urlPath) {
			return
		}
		c.Abort()
		encoding := preferredFrontendEncoding(c.GetHeader("Accept-Encoding"))
		if encoding == "" ||
			ginGzip.DefaultExcludedExtentions.Contains(path.Ext(urlPath)) ||
			strings.HasSuffix(urlPath, "/index.html") {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		key := precompressedFileKey{urlPath: urlPath, encoding: encoding}
		value, ok := cache.Load(key)
		if !ok {
			value, _ = cache.LoadOrStore(key, &precompressedFile{})
		}
		file := value.(*precompressedFile)
		file.once.Do(func() {
			f, err := frontendFS.Open(urlPath)
			if err != nil {
				file.err = err
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				file.err = err
				return
			}
			if info.IsDir() {
				file.err = errors.New("directory")
				return
			}
			raw, err := io.ReadAll(f)
			if err != nil {
				file.err = err
				return
			}
			var compressed bytes.Buffer
			var writer io.WriteCloser
			if encoding == "br" {
				writer = brotli.NewWriterLevel(&compressed, frontendBrotliLevel)
			} else {
				writer = gzip.NewWriter(&compressed)
			}
			if _, err := writer.Write(raw); err != nil {
				file.err = err
				return
			}
			if err := writer.Close(); err != nil {
				file.err = err
				return
			}
			// Match http.FileServer: extension first, then content sniffing.
			file.contentType = mime.TypeByExtension(path.Ext(urlPath))
			if file.contentType == "" {
				file.contentType = http.DetectContentType(raw)
			}
			file.body = bytes.Clone(compressed.Bytes())
		})
		if file.err != nil {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		// Range requests get the whole body; a server may ignore Range.
		header := c.Writer.Header()
		header.Set("Content-Encoding", encoding)
		header.Set("Vary", "Accept-Encoding")
		header.Set("Content-Type", file.contentType)
		header.Set("Content-Length", strconv.Itoa(len(file.body)))
		c.Writer.WriteHeader(http.StatusOK)
		if c.Request.Method != http.MethodHead {
			_, _ = c.Writer.Write(file.body)
		}
	}
}

// preferredFrontendEncoding picks the content encoding for a frontend build
// file from an Accept-Encoding header: "br" when accepted, otherwise "gzip"
// when accepted, otherwise "". Codings listed with q=0 are refused; other q
// values do not change the server's preference.
func preferredFrontendEncoding(acceptEncoding string) string {
	acceptsGzip := false
	for token := range strings.SplitSeq(acceptEncoding, ",") {
		coding, params, _ := strings.Cut(token, ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "br" && coding != "gzip" {
			continue
		}
		refused := false
		for param := range strings.SplitSeq(params, ";") {
			name, value, found := strings.Cut(param, "=")
			if !found || !strings.EqualFold(strings.TrimSpace(name), "q") {
				continue
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			refused = err == nil && q == 0
		}
		if refused {
			continue
		}
		if coding == "br" {
			return "br"
		}
		acceptsGzip = true
	}
	if acceptsGzip {
		return "gzip"
	}
	return ""
}
