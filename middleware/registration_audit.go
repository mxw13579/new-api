package middleware

import (
	"bytes"
	"net/url"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	AuditActionRegister             = "user.register"
	AuditActionRegisterVerification = "user.register.verification"

	registrationAuditContextKey = "registration_audit"
	// turnstileRejectionContextKey holds the reason code TurnstileCheck
	// rejected the request with, so audits can tell it from handler failures.
	turnstileRejectionContextKey = "turnstile_rejection"
)

// RegistrationAuditDetail is the non-secret context a registration handler
// adds to its audit entry. Passwords, verification codes, Turnstile tokens,
// request bodies and query strings never belong here.
type RegistrationAuditDetail struct {
	Method    string // password, or the OAuth provider used to sign up
	Reason    string // stable failure code; empty on success
	UserId    int    // the created account, 0 when none was created
	Username  string // the requested or created username
	Email     string // normalized address
	InviterId int    // resolved inviter, 0 when none
}

// RegistrationAudit records one entry per request that passes the endpoint's
// rate limit, including requests rejected by later middleware such as
// TurnstileCheck. The rate limit runs first so the write volume stays bounded
// by it under automated registration floods.
func RegistrationAudit(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// The verification endpoint carries the address in its query; the
		// handler replaces it with the validated value when it gets that far.
		detail := &RegistrationAuditDetail{Email: model.NormalizeEmail(c.Query("email"))}
		c.Set(registrationAuditContextKey, detail)
		writer := &auditResponseWriter{ResponseWriter: c.Writer, body: bytes.NewBuffer(nil), maxSize: 64 * 1024}
		c.Writer = writer
		c.Next()
		status := writer.Status()
		success := auditResponseSuccess(status, writer.body.Bytes())
		if !success && detail.Reason == "" {
			detail.Reason = c.GetString(turnstileRejectionContextKey)
			if detail.Reason == "" {
				detail.Reason = "request_rejected"
			}
		}
		RecordRegistrationAudit(c, action, status, success, *detail)
	}
}

// RegistrationAuditOf returns the detail RegistrationAudit will record. Without
// that middleware it returns a detached value, so handlers need no nil checks.
func RegistrationAuditOf(c *gin.Context) *RegistrationAuditDetail {
	if detail, ok := c.Value(registrationAuditContextKey).(*RegistrationAuditDetail); ok {
		return detail
	}
	return &RegistrationAuditDetail{}
}

// RecordRegistrationAudit writes a registration event. The method and failure
// reason are visible to the account owner; the email, inviter, requested
// username and request origin stay in audit_info, which only administrators see.
// A status of 0 takes the response status written so far.
func RecordRegistrationAudit(c *gin.Context, action string, status int, success bool, detail RegistrationAuditDetail) {
	params := model.AuditFields{}
	if detail.Method != "" {
		params["method"] = detail.Method
	}
	if !success && detail.Reason != "" {
		params["failure_reason"] = detail.Reason
	}
	requestParams := map[string]string{"inviter_id": strconv.Itoa(detail.InviterId)}
	if email := truncateAuditValue(detail.Email, 254); email != "" {
		requestParams["email"] = email
	}
	if username := truncateAuditValue(detail.Username, 64); username != "" {
		requestParams["username"] = username
	}
	auditInfo := &model.AuditRequestInfo{Route: c.FullPath(), Path: c.FullPath(), Success: success, Params: requestParams}
	if c.Request != nil {
		auditInfo.Method = c.Request.Method
		if referer := auditRequestOrigin(c.GetHeader("Referer")); referer != "" {
			requestParams["referer"] = referer
		}
		if origin := auditRequestOrigin(c.GetHeader("Origin")); origin != "" {
			requestParams["origin"] = origin
		}
	}
	if status == 0 && c.Writer != nil {
		status = c.Writer.Status()
	}
	auditInfo.Status = status
	content := "User registration"
	if action == AuditActionRegisterVerification {
		content = "Registration verification code request"
	}
	entry := model.AuditLog{
		ActorRole: common.RoleCommonUser, // An anonymous registrant can at most become a common user.
		Category:  model.AuditCategoryLogin,
		Action:    action,
		Content:   content,
		Status:    status,
		Success:   success,
		Other: model.AuditOther{
			Op:        &model.AuditOperation{Action: action, Params: params},
			AuditInfo: auditInfo,
		},
	}
	// Failed attempts belong to no account and stay out of every self view. An
	// account created before a later step failed is still attributed to it.
	if detail.UserId > 0 {
		entry.UserId, entry.Username = detail.UserId, detail.Username
	}
	model.RecordAuditLog(c, entry)
}

// auditRequestOrigin keeps the scheme, host and path of a client-supplied URL
// header. Query strings and fragments can carry tokens, so they are dropped.
func auditRequestOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "invalid"
	}
	kept := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}
	return truncateAuditValue(kept.String(), 255)
}

func truncateAuditValue(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
