package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// turnstileStub answers Cloudflare's siteverify call so TurnstileCheck runs
// its real path without network access, and keeps the tokens it received.
type turnstileStub struct {
	success bool
	tokens  []string
}

func (stub *turnstileStub) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := request.ParseForm(); err != nil {
		return nil, err
	}
	stub.tokens = append(stub.tokens, request.PostForm.Get("response"))
	body := `{"success":` + strconv.FormatBool(stub.success) + `}`
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

// setupRegistrationAudit returns the production chains after their rate
// limits, and the same chains without the audit middleware for comparison.
func setupRegistrationAudit(t *testing.T) (*model.User, *gin.Engine, *gin.Engine, *turnstileStub) {
	t.Helper()
	inviter, _ := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.AffiliateLog{}))
	require.NoError(t, model.DB.Model(inviter).Update("email", "taken@example.com").Error)
	previous := []bool{common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, common.TurnstileCheckEnabled, constant.GenerateDefaultToken, common.EmailDomainRestrictionEnabled}
	previousWhitelist, previousTransport := common.EmailDomainWhitelist, http.DefaultClient.Transport
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = true, true, true
	common.TurnstileCheckEnabled, constant.GenerateDefaultToken, common.EmailDomainRestrictionEnabled = false, false, false
	stub := &turnstileStub{}
	http.DefaultClient.Transport = stub
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = previous[0], previous[1], previous[2]
		common.TurnstileCheckEnabled, constant.GenerateDefaultToken, common.EmailDomainRestrictionEnabled = previous[3], previous[4], previous[5]
		common.EmailDomainWhitelist, http.DefaultClient.Transport = previousWhitelist, previousTransport
	})
	gin.SetMode(gin.TestMode)
	audited, bare := gin.New(), gin.New()
	audited.POST("/api/user/register", middleware.RegistrationAudit(middleware.AuditActionRegister), middleware.AnonymousRequestBodyLimit(), middleware.TurnstileCheck(), Register)
	audited.GET("/api/verification", middleware.RegistrationAudit(middleware.AuditActionRegisterVerification), middleware.TurnstileCheck(), SendEmailVerification)
	bare.POST("/api/user/register", middleware.AnonymousRequestBodyLimit(), middleware.TurnstileCheck(), Register)
	return inviter, audited, bare, stub
}

const registrationReferer = "https://landing.example/register?aff=audit-owner&token=referer-secret#fragment-secret"

func registrationRequest(router http.Handler, method, path, body, referer string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("User-Agent", "registration-bot/1.0")
	request.Header.Set("Referer", referer)
	request.Header.Set("Origin", "https://landing.example")
	request.RemoteAddr = "192.0.2.8:4567"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func latestAudit(t *testing.T) model.AuditLog {
	t.Helper()
	var entry model.AuditLog
	require.NoError(t, model.LOG_DB.Order("id DESC").First(&entry).Error)
	return entry
}

func TestRegistrationAuditRecordsEveryOutcomeWithoutSecrets(t *testing.T) {
	inviter, router, _, stub := setupRegistrationAudit(t)
	common.RegisterVerificationCodeWithKey("new@example.com", "246810", common.EmailVerificationPurpose)
	common.RegisterVerificationCodeWithKey("other@example.com", "135791", common.EmailVerificationPurpose)
	secrets := []string{"correct-horse-1", "246810", "135791", "000000", "turnstile-secret", "referer-secret", "fragment-secret", "userinfo-secret", "aff="}

	cases := []struct {
		name, path, body, referer     string
		turnstile                     bool
		reason, username, email, want string // want is the stored referer
	}{
		{
			name: "success", path: "/api/user/register", referer: registrationReferer, want: "https://landing.example/register",
			username: "new-user", email: "new@example.com",
			body: `{"username":"new-user","password":"correct-horse-1","email":" New@Example.com ","verification_code":"246810","aff_code":"audit-owner"}`,
		},
		{
			name: "wrong code", path: "/api/user/register", referer: registrationReferer, want: "https://landing.example/register",
			reason: "code_invalid", username: "second-user", email: "other@example.com",
			body: `{"username":"second-user","password":"correct-horse-1","email":"other@example.com","verification_code":"000000"}`,
		},
		{
			name: "username taken", path: "/api/user/register", referer: "not a url", want: "invalid",
			reason: "username_taken", username: "audit-owner", email: "other@example.com",
			body: `{"username":"audit-owner","password":"correct-horse-1","email":"other@example.com","verification_code":"135791"}`,
		},
		{
			name: "turnstile rejected", path: "/api/user/register?turnstile=turnstile-secret", turnstile: true,
			referer: "https://visitor:userinfo-secret@landing.example/register?x=1", want: "https://landing.example/register",
			reason: "turnstile_failed", body: `{"username":"third-user","password":"correct-horse-1"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			common.TurnstileCheckEnabled = tc.turnstile
			registrationRequest(router, "POST", tc.path, tc.body, tc.referer)
			entry := latestAudit(t)
			assert.Equal(t, middleware.AuditActionRegister, entry.Action)
			assert.Equal(t, model.AuditCategoryLogin, entry.Category)
			assert.Equal(t, common.RoleCommonUser, entry.ActorRole)
			assert.Equal(t, "192.0.2.8", entry.Ip)
			assert.Equal(t, "registration-bot/1.0", entry.UserAgent)
			assert.Equal(t, "/api/user/register", entry.Route)
			require.NotNil(t, entry.Other.Op)
			require.NotNil(t, entry.Other.AuditInfo)
			assert.Equal(t, "password", auditParam(t, entry.Other.Op.Params, "method"))
			info := entry.Other.AuditInfo.Params
			assert.Equal(t, tc.want, info["referer"])
			assert.Equal(t, "https://landing.example", info["origin"])
			assert.Equal(t, tc.username, info["username"])
			assert.Equal(t, tc.email, info["email"])
			assert.Equal(t, tc.reason == "", entry.Success)
			if tc.reason == "" {
				var created model.User
				require.NoError(t, model.DB.Where("username = ?", "new-user").First(&created).Error)
				assert.Equal(t, created.Id, entry.UserId)
				assert.Equal(t, "new-user", entry.Username)
				assert.Equal(t, inviter.Id, created.InviterId)
				assert.Equal(t, strconv.Itoa(inviter.Id), info["inviter_id"])
				assert.NotContains(t, entry.Other.Op.Params, "failure_reason")
			} else {
				assert.Zero(t, entry.UserId)
				assert.Empty(t, entry.Username)
				assert.Equal(t, "0", info["inviter_id"])
				assert.Equal(t, tc.reason, auditParam(t, entry.Other.Op.Params, "failure_reason"))
			}
			encoded, err := common.Marshal(entry)
			require.NoError(t, err)
			for _, secret := range secrets {
				assert.NotContains(t, string(encoded), secret)
			}
		})
	}
	assert.Equal(t, []string{"turnstile-secret"}, stub.tokens, "the token must reach the real verification path")

	// The new account sees its own registration without the admin-only request details.
	var created model.User
	require.NoError(t, model.DB.Where("username = ?", "new-user").First(&created).Error)
	logs, _, err := model.GetAuditLogs(model.AuditLogFilter{SelfView: true, UserId: created.Id}, 0, 10, common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Nil(t, logs[0].Other.AuditInfo)
	assert.Equal(t, "password", auditParam(t, logs[0].Other.Op.Params, "method"))
}

func TestRegistrationAuditLeavesResponsesUnchanged(t *testing.T) {
	_, audited, bare, _ := setupRegistrationAudit(t)
	for _, email := range []string{"audited@example.com", "bare@example.com"} {
		common.RegisterVerificationCodeWithKey(email, "246810", common.EmailVerificationPurpose)
	}
	for _, tc := range []struct{ name, code string }{{"success", "246810"}, {"wrong code", "000000"}} {
		t.Run(tc.name, func(t *testing.T) {
			body := func(name string) string {
				return `{"username":"` + name + `-` + tc.code + `","password":"correct-horse-1","email":"` + name + `@example.com","verification_code":"` + tc.code + `"}`
			}
			want := registrationRequest(bare, "POST", "/api/user/register", body("bare"), registrationReferer)
			got := registrationRequest(audited, "POST", "/api/user/register", body("audited"), registrationReferer)
			assert.Equal(t, want.Code, got.Code)
			assert.JSONEq(t, want.Body.String(), got.Body.String())
		})
	}
}

// auditParam decodes one stored op parameter, which reads back as raw JSON.
func auditParam(t *testing.T, params model.AuditFields, key string) string {
	t.Helper()
	raw, ok := params[key].(json.RawMessage)
	require.True(t, ok, key)
	var value string
	require.NoError(t, common.Unmarshal(raw, &value))
	return value
}

func TestRegistrationVerificationAuditExcludesQueryToken(t *testing.T) {
	_, router, _, stub := setupRegistrationAudit(t)
	stub.success = true
	common.TurnstileCheckEnabled = true
	for _, tc := range []struct{ name, email, reason, stored string }{
		{"email taken", "Taken@Example.com", "email_taken", "taken@example.com"},
		{"domain restricted", "visitor@blocked.example", "email_restricted", "visitor@blocked.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.EmailDomainRestrictionEnabled = tc.reason == "email_restricted"
			common.EmailDomainWhitelist = []string{"example.com"}
			registrationRequest(router, "GET", "/api/verification?email="+tc.email+"&turnstile=turnstile-query-secret", "", registrationReferer)
			entry := latestAudit(t)
			assert.Equal(t, middleware.AuditActionRegisterVerification, entry.Action)
			assert.False(t, entry.Success)
			assert.Equal(t, "/api/verification", entry.Route)
			assert.Zero(t, entry.UserId)
			assert.NotContains(t, entry.Other.Op.Params, "method")
			require.NotNil(t, entry.Other.AuditInfo)
			assert.Equal(t, tc.stored, entry.Other.AuditInfo.Params["email"])
			assert.Equal(t, "https://landing.example/register", entry.Other.AuditInfo.Params["referer"])
			assert.Equal(t, tc.reason, auditParam(t, entry.Other.Op.Params, "failure_reason"))
			encoded, err := common.Marshal(entry)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "turnstile-query-secret")
			assert.NotContains(t, string(encoded), "turnstile=")
		})
	}
	assert.Equal(t, []string{"turnstile-query-secret", "turnstile-query-secret"}, stub.tokens)
}

func TestOAuthRegistrationAuditSeparatesSignUpFromLogin(t *testing.T) {
	inviter, _, _, _ := setupRegistrationAudit(t)
	provider := &oauth.GitHubProvider{}
	identity := &oauth.OAuthUser{ProviderUserID: "github-501", Username: "oauth-new", Email: "OAuth@Example.com"}
	var created *model.User
	router := gin.New()
	// The callback route supplies the sign-up method, as in production.
	router.GET("/api/oauth/:provider", func(c *gin.Context) {
		user, _, err := findOrCreateOAuthUser(c, provider, identity, &oauth.OAuthToken{}, inviter.AffCode)
		require.NoError(t, err)
		created = user
	})
	for range 2 {
		request := httptest.NewRequest("GET", "/api/oauth/github?code=oauth-code-secret&state=oauth-state-secret", nil)
		request.Header.Set("Referer", "https://github.com/login/oauth/authorize?client_id=x#y")
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
	require.NotNil(t, created)

	var entries []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ?", middleware.AuditActionRegister).Find(&entries).Error)
	require.Len(t, entries, 1, "a returning OAuth login is not a registration")
	entry := entries[0]
	assert.True(t, entry.Success)
	assert.Equal(t, created.Id, entry.UserId)
	assert.Equal(t, "oauth:github", auditParam(t, entry.Other.Op.Params, "method"))
	require.NotNil(t, entry.Other.AuditInfo)
	assert.Equal(t, "oauth@example.com", entry.Other.AuditInfo.Params["email"])
	assert.Equal(t, strconv.Itoa(inviter.Id), entry.Other.AuditInfo.Params["inviter_id"])
	assert.Equal(t, "https://github.com/login/oauth/authorize", entry.Other.AuditInfo.Params["referer"])
	encoded, err := common.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "oauth-code-secret")
	assert.NotContains(t, string(encoded), "oauth-state-secret")
}
