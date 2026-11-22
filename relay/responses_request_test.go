package relay

import (
	"cmp"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrepareResponsesRequestDeepSeekEffectiveReasoning(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	settings.PassThroughRequestEnabled = false
	const input = `[{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"}]`
	for _, tc := range []struct {
		name, effort, override, endpoint         string
		passthrough, replaceInput, wantReasoning bool
		globalPassthrough                        bool
	}{
		{name: "override enables thinking", effort: "none", override: "high", wantReasoning: true},
		{name: "override disables thinking", effort: "high", override: "none"},
		{name: "override replaces history", effort: "high", override: "high", replaceInput: true, wantReasoning: true},
		{name: "no override", effort: "high", wantReasoning: true},
		{name: "thinking disabled", effort: "none"},
		{name: "third party endpoint", effort: "none", override: "high", endpoint: "https://other.example"},
		{name: "passthrough bypasses overrides and normalization", effort: "none", override: "high", passthrough: true},
		{name: "global passthrough bypasses normalization", effort: "high", globalPassthrough: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.PassThroughRequestEnabled = tc.globalPassthrough
			endpoint := tc.endpoint
			if endpoint == "" {
				endpoint = "https://API.DEEPSEEK.COM:443/v1"
			}
			raw := `{"model":"deepseek-flash","reasoning":{"effort":"` + tc.effort + `"},"input":` + input + `}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-flash")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, endpoint)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough})
			if tc.override != "" {
				ops := []any{map[string]any{"path": "reasoning.effort", "mode": "set", "value": tc.override}, map[string]any{"path": "vendor_extension", "mode": "set", "value": "preserved"}}
				if tc.replaceInput {
					var replacement any
					require.NoError(t, common.Unmarshal([]byte(input), &replacement))
					ops = append(ops, map[string]any{"path": "input", "mode": "set", "value": replacement})
				}
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{"operations": ops})
			}
			var req dto.OpenAIResponsesRequest
			require.NoError(t, common.Unmarshal([]byte(raw), &req))
			info := relaycommon.GenRelayInfoResponses(c, &req)
			_, body, closer, apiErr := PrepareResponsesRequest(c, info, &req)
			require.Nil(t, apiErr)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			wire, err := io.ReadAll(body)
			require.NoError(t, err)
			if tc.passthrough || tc.globalPassthrough {
				assert.JSONEq(t, raw, string(wire))
				return
			}
			wantEffort := tc.effort
			if tc.override != "" {
				wantEffort = tc.override
				assert.Equal(t, "preserved", gjson.GetBytes(wire, "vendor_extension").String())
			}
			assert.Equal(t, wantEffort, gjson.GetBytes(wire, "reasoning.effort").String())
			if tc.wantReasoning {
				assert.Equal(t, "reasoning", gjson.GetBytes(wire, "input.0.type").String())
				assert.Equal(t, " ", gjson.GetBytes(wire, "input.0.content.0.text").String())
				assert.Equal(t, "function_call", gjson.GetBytes(wire, "input.1.type").String())
			} else {
				assert.JSONEq(t, input, gjson.GetBytes(wire, "input").Raw)
			}
		})
	}
}

// finalizedEgressPlugin rejects a body whose first input item is reasoning,
// which only the DeepSeek finalizer adds to the test request.
const finalizedEgressPlugin = `export const meta = {apiVersion: 1, key: "rp-finalized-egress", name: "Finalized egress", version: "1.0.0", author: {name: "Test"}};
export function egress(ctx, req) {
  const input = req.body && req.body.input;
  if (Array.isArray(input) && input.length && input[0].type === "reasoning") {
    return {action: "reject", code: "finalized.seen", message: "Egress saw the finalized body"};
  }
  return {action: "continue"};
}`

func TestPrepareResponsesRequestEgressReadsTheFinalizedBody(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	settings.PassThroughRequestEnabled = false
	require.NoError(t, i18n.Init())
	plugin, err := jsplugin.DefaultRegistry.Register(finalizedEgressPlugin, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: `[{"id":"fe","name":"Finalized","plugin":"rp-finalized-egress","enabled":true,"global":true}]`}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
	})

	raw := `{"model":"deepseek-flash","reasoning":{"effort":"high"},"input":[{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-flash")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://api.deepseek.com")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	_, _, apiErr := requestplugin.Ingress(c, types.RelayFormatOpenAIResponses)
	require.Nil(t, apiErr)
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(raw), &req))
	info := relaycommon.GenRelayInfoResponses(c, &req)
	_, _, closer, apiErr := PrepareResponsesRequest(c, info, &req)
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	require.NotNil(t, apiErr, "egress must read the reasoning item the finalizer added")
	assert.Equal(t, types.ErrorCodeRequestRejected, apiErr.GetErrorCode())
}

func TestTextBillingUsesTheSentServiceTier(t *testing.T) {
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	settings.PassThroughRequestEnabled = false
	const expr = `(tier("base", p * 10)) * (param("service_tier") == "fast" ? 2 : 1) * (param("service_tier") == "ultrafast" ? 6 : 1)`
	// P=1000 estimates base 10000, which is 5000 quota at a 500000 unit.
	const quotaPerUnit = 500_000.0
	const responsesBody = `{"model":"gpt-5","input":"hi"}`
	const responsesFast = `{"model":"gpt-5","input":"hi","service_tier":"fast"}`
	type attempt struct {
		override               string
		allowTier, passThrough bool
	}
	for _, tc := range []struct {
		name, endpoint, body, contentType string
		attempts                          []attempt
		insufficient                      bool
		// wantTier is the tier the last attempt sends and the billing input
		// carries; "" means none.
		wantTier  string
		wantQuota int
	}{
		{name: "override writes fast", body: responsesBody, attempts: []attempt{{override: "fast"}}, wantTier: "fast", wantQuota: 10000},
		{name: "filtered client tier", body: responsesFast, attempts: []attempt{{}}, wantQuota: 5000},
		{name: "override replaces the client tier", body: responsesFast, attempts: []attempt{{allowTier: true, override: "ultrafast"}}, wantTier: "ultrafast", wantQuota: 30000},
		{name: "pass-through keeps the client tier", body: responsesFast, attempts: []attempt{{passThrough: true, override: "ultrafast"}}, wantTier: "fast", wantQuota: 10000},
		{name: "pass-through retry after an override", body: responsesBody, attempts: []attempt{{override: "fast"}, {passThrough: true}}, wantQuota: 5000},
		{name: "non-JSON content type with a forced tier", body: responsesBody, contentType: "application/x-www-form-urlencoded", attempts: []attempt{{override: "fast"}}, wantTier: "fast", wantQuota: 10000},
		{name: "websocket create", endpoint: "websocket", body: responsesBody, attempts: []attempt{{override: "fast"}}, wantTier: "fast", wantQuota: 10000},
		{name: "chat completions override", endpoint: "chat", body: `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`, attempts: []attempt{{override: "fast"}}, wantTier: "fast", wantQuota: 10000},
		{name: "claude messages override", endpoint: "claude", body: `{"model":"claude-x","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, attempts: []attempt{{override: "fast"}}, wantTier: "fast", wantQuota: 10000},
		{name: "chat completions pass-through retry", endpoint: "chat", body: `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`, attempts: []attempt{{override: "fast"}, {passThrough: true}}, wantQuota: 5000},
		{name: "claude messages pass-through retry", endpoint: "claude", body: `{"model":"claude-x","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, attempts: []attempt{{override: "fast"}, {passThrough: true}}, wantQuota: 5000},
		{name: "gemini override", endpoint: "gemini", body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, attempts: []attempt{{override: "fast"}}, wantTier: "fast", wantQuota: 10000},
		{name: "gemini pass-through retry", endpoint: "gemini", body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, attempts: []attempt{{override: "fast"}, {passThrough: true}}, wantQuota: 5000},
		{name: "uncovered raise blocks upstream", body: responsesBody, attempts: []attempt{{override: "fast"}}, insufficient: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan []byte, len(tc.attempts))
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err == nil {
					received <- body
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture upstream failure","type":"upstream_error"}}`)
			}))
			t.Cleanup(upstream.Close)
			path, channelType := "/v1/responses", constant.ChannelTypeOpenAI
			switch tc.endpoint {
			case "chat":
				path = "/v1/chat/completions"
			case "claude":
				path, channelType = "/v1/messages", constant.ChannelTypeAnthropic
			case "gemini":
				path, channelType = "/v1beta/models/gemini-x:generateContent", constant.ChannelTypeGemini
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", cmp.Or(tc.contentType, "application/json"))
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			model := cmp.Or(gjson.Get(tc.body, "model").String(), "gemini-x")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, model)
			common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "fixture-key")
			var info *relaycommon.RelayInfo
			var responsesReq dto.OpenAIResponsesRequest
			switch tc.endpoint {
			case "chat":
				var req dto.GeneralOpenAIRequest
				require.NoError(t, common.Unmarshal([]byte(tc.body), &req))
				info = relaycommon.GenRelayInfoOpenAI(c, &req)
			case "claude":
				var req dto.ClaudeRequest
				require.NoError(t, common.Unmarshal([]byte(tc.body), &req))
				info = relaycommon.GenRelayInfoClaude(c, &req)
			case "gemini":
				var req dto.GeminiChatRequest
				require.NoError(t, common.Unmarshal([]byte(tc.body), &req))
				info = relaycommon.GenRelayInfoGemini(c, &req)
			default:
				require.NoError(t, common.Unmarshal([]byte(tc.body), &responsesReq))
				info = relaycommon.GenRelayInfoResponses(c, &responsesReq)
			}
			// The initial reservation prices the client's own tier from the
			// body PrepareRequestBilling loads.
			input, err := helper.ResolveIncomingBillingExprRequestInput(c, info, true)
			require.NoError(t, err)
			if tc.contentType != "" {
				require.Nil(t, input.Body, "a non-JSON body is not loaded for param()")
			}
			cost, _, err := billingexpr.RunExprByHashWithRequest(expr, billingexpr.ExprHashString(expr), billingexpr.TokenParams{P: 1000, Len: 1000}, input)
			require.NoError(t, err)
			initial := common.QuotaRound(cost / 1_000_000 * quotaPerUnit)
			reservation := &imageReservation{held: initial, limit: 1_000_000}
			if tc.insufficient {
				reservation.limit = reservation.held
			}
			info.Billing = reservation
			info.FinalPreConsumedQuota = initial
			info.BillingRequestInput = &input
			info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expr, ExprHash: billingexpr.ExprHashString(expr),
				GroupRatio: 1, EstimatedPromptTokens: 1000, PreConsumeMultiplier: 1, QuotaPerUnit: quotaPerUnit,
				EstimatedQuotaBeforeGroup: float64(initial), EstimatedQuotaAfterGroup: initial, EstimatedTier: "base"}
			info.PriceData = hosttypes.PriceData{QuotaToPreConsume: initial, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}

			var sent []byte
			for _, at := range tc.attempts {
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: at.passThrough})
				common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowServiceTier: at.allowTier})
				var override map[string]any
				if at.override != "" {
					override = map[string]any{"operations": []any{map[string]any{"path": "service_tier", "mode": "set", "value": at.override}}}
				}
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, override)
				if tc.endpoint == "websocket" {
					payload, apiErr := buildResponsesWSCreatePayload(c, info, responsesReq, nil, "")
					require.Nil(t, apiErr)
					sent = payload
					continue
				}
				var apiErr *types.NewAPIError
				switch tc.endpoint {
				case "chat":
					apiErr = TextHelper(c, info)
				case "claude":
					apiErr = ClaudeHelper(c, info)
				case "gemini":
					apiErr = GeminiHelper(c, info)
				default:
					apiErr = ResponsesHelper(c, info)
				}
				require.NotNil(t, apiErr)
				if tc.insufficient {
					assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
					assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
					assert.Empty(t, received, "no upstream request before the raised reservation")
					assert.Equal(t, tc.body, string(info.BillingRequestInput.Body))
					assert.Equal(t, initial, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
					return
				}
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode, apiErr.Error())
				require.Len(t, received, 1)
				sent = <-received
			}
			assert.Equal(t, tc.wantTier, gjson.GetBytes(sent, "service_tier").String())
			tier := gjson.GetBytes(info.BillingRequestInput.Body, "service_tier")
			assert.Equal(t, tc.wantTier, tier.String())
			assert.Equal(t, tc.wantTier != "", tier.Exists())
			if tc.contentType == "" {
				assert.Equal(t, gjson.Get(tc.body, "model").String(), gjson.GetBytes(info.BillingRequestInput.Body, "model").String())
			}
			assert.Equal(t, tc.wantQuota, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
			assert.GreaterOrEqual(t, reservation.held, max(initial, tc.wantQuota), "a lower estimate keeps the reservation")
		})
	}
}
