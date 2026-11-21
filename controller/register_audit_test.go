package controller

import (
	"encoding/json"
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

func setupRegistrationAudit(t *testing.T) (*model.User, *gin.Engine) {
	t.Helper()
	inviter, _ := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.AffiliateLog{}))
	require.NoError(t, model.DB.Model(inviter).Update("email", "taken@example.com").Error)
	previous := []bool{common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, common.TurnstileCheckEnabled, constant.GenerateDefaultToken}
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = true, true, true
	common.TurnstileCheckEnabled, constant.GenerateDefaultToken = false, false
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = previous[0], previous[1], previous[2]
		common.TurnstileCheckEnabled, constant.GenerateDefaultToken = previous[3], previous[4]
	})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// Mirrors the production chains after their rate limits.
	router.POST("/api/user/register", middleware.RegistrationAudit(middleware.AuditActionRegister), middleware.AnonymousRequestBodyLimit(), middleware.TurnstileCheck(), Register)
	router.GET("/api/verification", middleware.RegistrationAudit(middleware.AuditActionRegisterVerification), middleware.TurnstileCheck(), SendEmailVerification)
	return inviter, router
}

func registrationRequest(router http.Handler, method, path, body string) {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("User-Agent", "registration-bot/1.0")
	request.Header.Set("Referer", "https://landing.example/register?aff=audit-owner&token=referer-secret#fragment-secret")
	request.Header.Set("Origin", "https://landing.example")
	request.RemoteAddr = "192.0.2.8:4567"
	router.ServeHTTP(httptest.NewRecorder(), request)
}

func latestAudit(t *testing.T) model.AuditLog {
	t.Helper()
	var entry model.AuditLog
	require.NoError(t, model.LOG_DB.Order("id DESC").First(&entry).Error)
	return entry
}

func TestRegistrationAuditRecordsEveryOutcomeWithoutSecrets(t *testing.T) {
	inviter, router := setupRegistrationAudit(t)
	common.RegisterVerificationCodeWithKey("new@example.com", "246810", common.EmailVerificationPurpose)
	common.RegisterVerificationCodeWithKey("other@example.com", "135791", common.EmailVerificationPurpose)
	secrets := []string{"correct-horse-1", "246810", "135791", "000000", "turnstile-secret", "referer-secret", "fragment-secret", "aff="}

	cases := []struct {
		name      string
		turnstile bool
		body      string
		reason    string
		username  string
		email     string
	}{
		{
			name: "success", username: "new-user", email: "new@example.com",
			body: `{"username":"new-user","password":"correct-horse-1","email":" New@Example.com ","verification_code":"246810","aff_code":"audit-owner"}`,
		},
		{
			name: "wrong code", reason: "code_invalid", username: "second-user", email: "other@example.com",
			body: `{"username":"second-user","password":"correct-horse-1","email":"other@example.com","verification_code":"000000"}`,
		},
		{
			name: "username taken", reason: "username_taken", username: "audit-owner", email: "other@example.com",
			body: `{"username":"audit-owner","password":"correct-horse-1","email":"other@example.com","verification_code":"135791"}`,
		},
		{
			name: "turnstile rejected", turnstile: true, reason: "turnstile_missing",
			body: `{"username":"third-user","password":"correct-horse-1","turnstile":"turnstile-secret"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			common.TurnstileCheckEnabled = tc.turnstile
			registrationRequest(router, "POST", "/api/user/register", tc.body)
			entry := latestAudit(t)
			assert.Equal(t, middleware.AuditActionRegister, entry.Action)
			assert.Equal(t, model.AuditCategoryLogin, entry.Category)
			assert.Equal(t, common.RoleCommonUser, entry.ActorRole)
			assert.Equal(t, "192.0.2.8", entry.Ip)
			assert.Equal(t, "registration-bot/1.0", entry.UserAgent)
			assert.Equal(t, "/api/user/register", entry.Route)
			require.NotNil(t, entry.Other.Op)
			require.NotNil(t, entry.Other.AuditInfo)
			info := entry.Other.AuditInfo.Params
			assert.Equal(t, "https://landing.example/register", info["referer"])
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

	// The new account sees its own registration without the admin-only request details.
	var created model.User
	require.NoError(t, model.DB.Where("username = ?", "new-user").First(&created).Error)
	logs, _, err := model.GetAuditLogs(model.AuditLogFilter{SelfView: true, UserId: created.Id}, 0, 10, common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Nil(t, logs[0].Other.AuditInfo)
	assert.Equal(t, "password", auditParam(t, logs[0].Other.Op.Params, "method"))
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
	_, router := setupRegistrationAudit(t)
	registrationRequest(router, "GET", "/api/verification?email=Taken@Example.com&turnstile=turnstile-query-secret", "")
	entry := latestAudit(t)
	assert.Equal(t, middleware.AuditActionRegisterVerification, entry.Action)
	assert.False(t, entry.Success)
	assert.Equal(t, "/api/verification", entry.Route)
	assert.Zero(t, entry.UserId)
	require.NotNil(t, entry.Other.AuditInfo)
	assert.Equal(t, "taken@example.com", entry.Other.AuditInfo.Params["email"])
	assert.Equal(t, "https://landing.example/register", entry.Other.AuditInfo.Params["referer"])
	assert.Equal(t, "email_taken", auditParam(t, entry.Other.Op.Params, "failure_reason"))
	encoded, err := common.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "turnstile-query-secret")
	assert.NotContains(t, string(encoded), "turnstile=")
}

func TestOAuthRegistrationAuditSeparatesSignUpFromLogin(t *testing.T) {
	inviter, _ := setupRegistrationAudit(t)
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
