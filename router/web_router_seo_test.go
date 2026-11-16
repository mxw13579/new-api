package router

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	seoTestPageBefore = `<!doctype html><html lang="zh-CN"><head>
    <meta charset="UTF-8" />
    `
	seoTestPageAfter = `
    <meta name="theme-color" content="#fff" />
    <script>document.documentElement.classList.add('light')</script>
    <!--umami-->
    <script type="module" src="/static/js/index.js"></script>
</head><body><div id="root"></div></body></html>`
	seoTestIndexPage = seoTestPageBefore + "<!--seo:start-->\n    <title>Default</title>\n    <!--seo:end-->" + seoTestPageAfter
)

func newSEOTestRouter(t *testing.T, systemName, serverAddress, logo string) *gin.Engine {
	t.Helper()
	originalName, originalAddress, originalLogo := common.SystemName, system_setting.ServerAddress, common.Logo
	common.SystemName, system_setting.ServerAddress, common.Logo = systemName, serverAddress, logo
	t.Cleanup(func() {
		common.SystemName, system_setting.ServerAddress, common.Logo = originalName, originalAddress, originalLogo
	})
	engine := gin.New()
	SetWebRouter(engine, WebAssets{IndexPage: []byte(seoTestIndexPage)}, func(c *gin.Context) { c.Next() })
	return engine
}

func seoBlock(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, "<head>")
	require.True(t, ok)
	head, _, ok := strings.Cut(rest, `<meta name="theme-color"`)
	require.True(t, ok)
	assert.NotContains(t, head, "<!--seo:")
	assert.NotContains(t, head, "Default")
	return head
}

func TestWebFallbackPublicPagesCarryPageMeta(t *testing.T) {
	engine := newSEOTestRouter(t, "Muze AI", "https://example.com/", "")
	cases := []struct {
		path, title, description, canonical string
	}{
		{"/", "Muze AI · Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API", "Muze AI 提供 Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API 接入，一个端点和密钥即可用于 Claude Code、Codex、Cursor 等开发工具，按量计费，价格透明。", "https://example.com/"},
		{"/pricing", "模型广场与价格 · Muze AI", "查看 Muze AI 已上线的全部模型及价格，按供应商、分组和类型筛选，比较不同模型的计费方式。", "https://example.com/pricing"},
		{"/about", "关于 · Muze AI", "了解 Muze AI 的服务介绍与联系方式。", "https://example.com/about"},
		{"/privacy-policy", "隐私政策 · Muze AI", "Muze AI 隐私政策。", "https://example.com/privacy-policy"},
		{"/user-agreement", "用户协议 · Muze AI", "Muze AI 用户协议。", "https://example.com/user-agreement"},
		{"/pricing/?vendor=openai", "模型广场与价格 · Muze AI", "查看 Muze AI 已上线的全部模型及价格，按供应商、分组和类型筛选，比较不同模型的计费方式。", "https://example.com/pricing"},
		{"/?aff=abc", "Muze AI · Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API", "Muze AI 提供 Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API 接入，一个端点和密钥即可用于 Claude Code、Codex、Cursor 等开发工具，按量计费，价格透明。", "https://example.com/"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			response := performPluginRequest(engine, http.MethodGet, tc.path)
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
			head := seoBlock(t, response.Body.String())
			assert.Contains(t, head, "<title>"+tc.title+"</title>")
			assert.Contains(t, head, `<meta name="title" content="`+tc.title+`" />`)
			assert.Contains(t, head, `<meta name="description" content="`+tc.description+`" />`)
			assert.Contains(t, head, `<meta property="og:title" content="`+tc.title+`" />`)
			assert.Contains(t, head, `<meta name="twitter:description" content="`+tc.description+`" />`)
			assert.Contains(t, head, `<link rel="canonical" href="`+tc.canonical+`" />`)
			assert.Contains(t, head, `<meta property="og:url" content="`+tc.canonical+`" />`)
			assert.Contains(t, head, `<meta property="og:image" content="https://example.com/logo.png" />`)
			assert.Contains(t, head, `<meta name="twitter:image" content="https://example.com/logo.png" />`)
			assert.Contains(t, head, `<meta property="og:site_name" content="Muze AI" />`)
			assert.Contains(t, head, `<meta property="og:locale" content="zh_CN" />`)
			assert.NotContains(t, head, "noindex")
		})
	}
}

func TestWebFallbackPrivateAndUnknownPagesAreNoindex(t *testing.T) {
	engine := newSEOTestRouter(t, "Muze AI", "https://example.com", "")
	homeDescription := "Muze AI 提供 Claude、Codex、Gemini 与 OpenAI 兼容模型的统一 API 接入，一个端点和密钥即可用于 Claude Code、Codex、Cursor 等开发工具，按量计费，价格透明。"
	cases := []struct {
		path   string
		status int
	}{
		{"/dashboard", http.StatusOK},
		{"/dashboard/overview", http.StatusOK},
		{"/sign-in?redirect=%2Fkeys", http.StatusOK},
		{"/pricing/gpt-4o", http.StatusOK},
		{"/console/token", http.StatusOK},
		{"/no-such-page", http.StatusNotFound},
		{"/no-such-page/deeper/", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			response := performPluginRequest(engine, http.MethodGet, tc.path)
			require.Equal(t, tc.status, response.Code)
			assert.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
			assert.Contains(t, response.Body.String(), `<div id="root"></div>`)
			head := seoBlock(t, response.Body.String())
			assert.Contains(t, head, "<title>Muze AI</title>")
			assert.Contains(t, head, `<meta name="description" content="`+homeDescription+`" />`)
			assert.Contains(t, head, `<meta name="robots" content="noindex" />`)
			assert.NotContains(t, head, "canonical")
			assert.NotContains(t, head, "og:url")
			assert.Contains(t, head, `<meta property="og:image" content="https://example.com/logo.png" />`)
		})
	}
}

func TestWebFallbackInvalidServerAddressOmitsURLTags(t *testing.T) {
	cases := []struct{ name, systemName, address, logo string }{
		{"empty address and name", "", "", ""},
		{"localhost default", "Muze AI", "http://localhost:3000", ""},
		{"ipv4 loopback", "Muze AI", "http://127.0.0.1:3000/", ""},
		{"ipv6 loopback", "Muze AI", "http://[::1]:3000", ""},
		{"missing scheme", "Muze AI", "example.com", ""},
		{"unsupported scheme", "Muze AI", "ftp://example.com", ""},
		{"missing host", "Muze AI", "https://", ""},
		{"absolute logo without site address", "Muze AI", "", "https://cdn.example.com/logo.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newSEOTestRouter(t, tc.systemName, tc.address, tc.logo)
			for _, path := range []string{"/", "/pricing", "/dashboard"} {
				response := performPluginRequest(engine, http.MethodGet, path)
				require.Equal(t, http.StatusOK, response.Code)
				head := seoBlock(t, response.Body.String())
				assert.Contains(t, head, `<meta property="og:site_name" content="Muze AI" />`)
				for _, tag := range []string{"canonical", "og:url", "og:image", "twitter:image"} {
					assert.NotContains(t, head, tag, path)
				}
			}
		})
	}
}

func TestWebFallbackImageUsesAbsoluteLogo(t *testing.T) {
	cases := []struct{ logo, image string }{
		{"https://cdn.example.com/brand.png", "https://cdn.example.com/brand.png"},
		{"http://cdn.example.com/brand.png", "http://cdn.example.com/brand.png"},
		{"/static/brand.png", "https://example.com/logo.png"},
		{"data:image/png;base64,AAAA", "https://example.com/logo.png"},
		{"", "https://example.com/logo.png"},
	}
	for _, tc := range cases {
		t.Run(tc.logo, func(t *testing.T) {
			engine := newSEOTestRouter(t, "Muze AI", "https://example.com", tc.logo)
			head := seoBlock(t, performPluginRequest(engine, http.MethodGet, "/").Body.String())
			assert.Contains(t, head, `<meta property="og:image" content="`+tc.image+`" />`)
			assert.Contains(t, head, `<meta name="twitter:image" content="`+tc.image+`" />`)
		})
	}
}

func TestWebFallbackReplacesOnlyTheSEOBlock(t *testing.T) {
	engine := newSEOTestRouter(t, "Muze AI", "https://example.com", "")
	response := performPluginRequest(engine, http.MethodGet, "/about")
	require.Equal(t, http.StatusOK, response.Code)
	expectedBlock := strings.Join([]string{
		`<title>关于 · Muze AI</title>`,
		`<meta name="title" content="关于 · Muze AI" />`,
		`<meta name="description" content="了解 Muze AI 的服务介绍与联系方式。" />`,
		`<link rel="canonical" href="https://example.com/about" />`,
		`<meta property="og:url" content="https://example.com/about" />`,
		`<meta property="og:title" content="关于 · Muze AI" />`,
		`<meta property="og:description" content="了解 Muze AI 的服务介绍与联系方式。" />`,
		`<meta property="og:type" content="website" />`,
		`<meta property="og:site_name" content="Muze AI" />`,
		`<meta property="og:locale" content="zh_CN" />`,
		`<meta name="twitter:card" content="summary" />`,
		`<meta name="twitter:title" content="关于 · Muze AI" />`,
		`<meta name="twitter:description" content="了解 Muze AI 的服务介绍与联系方式。" />`,
		`<meta property="og:image" content="https://example.com/logo.png" />`,
		`<meta name="twitter:image" content="https://example.com/logo.png" />`,
	}, "\n    ")
	assert.Equal(t, seoTestPageBefore+expectedBlock+seoTestPageAfter, response.Body.String())
}

func TestWebFallbackEscapesConfiguredValues(t *testing.T) {
	engine := newSEOTestRouter(t, `A<b>&"C`, `https://example.com/"><script>`, "")
	response := performPluginRequest(engine, http.MethodGet, "/about")
	require.Equal(t, http.StatusOK, response.Code)
	head := seoBlock(t, response.Body.String())
	assert.Contains(t, head, "<title>关于 · A&lt;b&gt;&amp;&#34;C</title>")
	assert.Contains(t, head, `<meta property="og:site_name" content="A&lt;b&gt;&amp;&#34;C" />`)
	assert.Contains(t, head, `<link rel="canonical" href="https://example.com/&#34;&gt;&lt;script&gt;/about" />`)
	assert.NotContains(t, head, "<b>")
	assert.NotContains(t, head, "<script>")
}

// A new top-level frontend route must be added to frontendRouteSegments, or
// the server would answer it with a 404 status.
func TestFrontendRouteSegmentsCoverRouteTree(t *testing.T) {
	source, err := os.ReadFile("../web/src/routeTree.gen.ts")
	require.NoError(t, err)
	matches := regexp.MustCompile(`fullPath: '([^']*)'`).FindAllStringSubmatch(string(source), -1)
	require.NotEmpty(t, matches)
	for _, match := range matches {
		segment, _, _ := strings.Cut(strings.TrimPrefix(match[1], "/"), "/")
		if segment == "" {
			continue
		}
		assert.NotContains(t, segment, "$", "dynamic first segment in %s needs a different 404 rule", match[1])
		assert.Contains(t, frontendRouteSegments, segment, "first segment of %s", match[1])
	}
}
