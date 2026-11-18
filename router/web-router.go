package router

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
}

const defaultSiteName = "Muze AI"

// publicPageMeta holds the indexable pages; %s is the site name.
var publicPageMeta = map[string]struct{ title, description string }{
	"/": {
		title:       "%s · Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API",
		description: "%s 提供 Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API 接入，一个端点和密钥即可用于 Claude Code、Codex、Cursor 等开发工具，按量计费，价格透明。",
	},
	"/pricing": {
		title:       "模型广场与价格 · %s",
		description: "查看 %s 已上线的全部模型及价格，按供应商、分组和类型筛选，比较不同模型的计费方式。",
	},
	"/about":          {title: "关于 · %s", description: "了解 %s 的服务介绍与联系方式。"},
	"/privacy-policy": {title: "隐私政策 · %s", description: "%s 隐私政策。"},
	"/user-agreement": {title: "用户协议 · %s", description: "%s 用户协议。"},
}

// frontendRouteSegments lists the first path segment of every route in
// web/src/routes (see web/src/routeTree.gen.ts). Any other path gets a 404
// status. TestFrontendRouteSegmentsCoverRouteTree keeps it in sync.
var frontendRouteSegments = []string{
	// Public pages.
	"about", "pricing", "privacy-policy", "user-agreement", "rankings",
	// (auth) group.
	"forgot-password", "oauth", "otp", "register", "reset", "sign-in", "sign-up", "user",
	// (errors) group.
	"401", "403", "404", "500", "503",
	// Setup wizard.
	"setup",
	// _authenticated group.
	"admin-invoices", "channels", "chat", "chat2link", "dashboard", "errors",
	"invoice-settings", "invoices", "keys", "models", "playground", "profile",
	"redemption-codes", "request-processing", "security", "subscriptions", "system-info",
	"system-settings", "task-plugins", "usage-logs", "users", "wallet",
	// Legacy paths redirected by web/src/lib/legacy-route.ts.
	"console", "login", "forbidden",
}

var (
	seoStartMarker = []byte("<!--seo:start-->")
	seoEndMarker   = []byte("<!--seo:end-->")
)

func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")
	compress := gzip.Gzip(gzip.DefaultCompression)

	// Split the page once around the seo markers; without them the page is
	// served unchanged.
	pageHead, pageRest, hasStart := bytes.Cut(assets.IndexPage, seoStartMarker)
	_, pageTail, hasEnd := bytes.Cut(pageRest, seoEndMarker)
	hasSEOBlock := hasStart && hasEnd

	router.NoRoute(
		pluginDispatcher,
		middleware.RouteTag("web"),
		func(c *gin.Context) {
			// ServeFrontendFiles sends build files already compressed.
			if !frontendFS.Exists("/", c.Request.URL.Path) {
				compress(c)
			}
		},
		middleware.AccessTokenAudit(),
		middleware.GlobalWebRateLimit(frontendFS),
		middleware.Cache(),
		middleware.ServeFrontendFiles(frontendFS),
		func(c *gin.Context) {
			if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
				controller.RelayNotFound(c)
				return
			}

			path := strings.TrimRight(c.Request.URL.Path, "/")
			if path == "" {
				path = "/"
			}
			firstSegment, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
			status := http.StatusOK
			if path != "/" && !slices.Contains(frontendRouteSegments, firstSegment) {
				status = http.StatusNotFound
			}

			c.Header("Cache-Control", "no-cache")
			if !hasSEOBlock {
				c.Data(status, "text/html; charset=utf-8", assets.IndexPage)
				return
			}

			siteName := common.SystemName
			if strings.TrimSpace(siteName) == "" {
				siteName = defaultSiteName
			}
			// Only an absolute http(s) address on a public host counts as
			// configured; the in-memory default is http://localhost:3000.
			siteURL := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
			if parsed, ok := parseAbsoluteHTTPURL(siteURL); !ok || parsed.Hostname() == "localhost" || net.ParseIP(parsed.Hostname()).IsLoopback() {
				siteURL = ""
			}
			imageURL := siteURL + "/logo.png"
			if logo := strings.TrimSpace(common.Logo); logo != "" {
				if _, ok := parseAbsoluteHTTPURL(logo); ok {
					imageURL = logo
				}
			}
			title := siteName
			description := fmt.Sprintf(publicPageMeta["/"].description, siteName)
			meta, isPublic := publicPageMeta[path]
			if isPublic {
				title = fmt.Sprintf(meta.title, siteName)
				description = fmt.Sprintf(meta.description, siteName)
			}

			title, description = html.EscapeString(title), html.EscapeString(description)
			tags := []string{
				"<title>" + title + "</title>",
				`<meta name="title" content="` + title + `" />`,
				`<meta name="description" content="` + description + `" />`,
			}
			if !isPublic {
				tags = append(tags, `<meta name="robots" content="noindex" />`)
			}
			if isPublic && siteURL != "" {
				pageURL := html.EscapeString(siteURL + path)
				tags = append(tags,
					`<link rel="canonical" href="`+pageURL+`" />`,
					`<meta property="og:url" content="`+pageURL+`" />`,
				)
			}
			tags = append(tags,
				`<meta property="og:title" content="`+title+`" />`,
				`<meta property="og:description" content="`+description+`" />`,
				`<meta property="og:type" content="website" />`,
				`<meta property="og:site_name" content="`+html.EscapeString(siteName)+`" />`,
				`<meta property="og:locale" content="zh_CN" />`,
				`<meta name="twitter:card" content="summary" />`,
				`<meta name="twitter:title" content="`+title+`" />`,
				`<meta name="twitter:description" content="`+description+`" />`,
			)
			if siteURL != "" {
				imageURL = html.EscapeString(imageURL)
				tags = append(tags,
					`<meta property="og:image" content="`+imageURL+`" />`,
					`<meta name="twitter:image" content="`+imageURL+`" />`,
				)
			}
			block := strings.Join(tags, "\n    ")

			page := make([]byte, 0, len(pageHead)+len(block)+len(pageTail))
			page = append(page, pageHead...)
			page = append(page, block...)
			page = append(page, pageTail...)
			c.Data(status, "text/html; charset=utf-8", page)
		},
	)
}

// parseAbsoluteHTTPURL reports whether raw is an absolute http or https URL
// with a host, the only form usable in canonical and og:* URL tags.
func parseAbsoluteHTTPURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, false
	}
	return parsed, true
}
