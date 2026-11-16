package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccount_IsCodexResponsesOnlyEnabled(t *testing.T) {
	cases := []struct {
		name    string
		account *Account
		want    bool
	}{
		{"OAuth 账号开启", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_responses_only": true}}, true},
		{"OAuth 账号显式关闭", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_responses_only": false}}, false},
		{"字段缺失默认关闭", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{}}, false},
		{"类型不对按关闭处理", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_responses_only": "true"}}, false},
		{"APIKey 账号不生效", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"codex_responses_only": true}}, false},
		{"非 OpenAI 账号不生效", &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"codex_responses_only": true}}, false},
		{"空 extra", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, false},
		{"nil 账号", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.account.IsCodexResponsesOnlyEnabled())
		})
	}
}

func newCodexResponsesOnlyTestContext(path string) (*httptest.ResponseRecorder, *gin.Context) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(nil))
	return rec, c
}

func codexResponsesOnlyTestAccount() *Account {
	return &Account{
		ID:       48,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_cli_only": true, "codex_responses_only": true},
	}
}

// chat 入口：开关开启时在访问上游之前就地拒绝，并告诉客户端该改走哪个协议。
func TestOpenAIGatewayService_ForwardAsChatCompletions_CodexResponsesOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hi"}]}`)

	t.Run("官方客户端走 chat 也被拒绝", func(t *testing.T) {
		rec, c := newCodexResponsesOnlyTestContext("/v1/chat/completions")
		svc := &OpenAIGatewayService{codexDetector: &stubCodexRestrictionDetector{result: CodexClientRestrictionDetectionResult{
			Enabled: true, Matched: true, Reason: "stub",
		}}}

		result, err := svc.ForwardAsChatCompletions(context.Background(), c, codexResponsesOnlyTestAccount(), body, "", "")

		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
		require.Equal(t, CodexResponsesOnlyErrorCode, gjson.Get(rec.Body.String(), "error.code").String())
		require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "/v1/responses")
		reason, _ := c.Get(OpsClientBusinessLimitedReasonKey)
		require.Equal(t, OpsClientBusinessLimitedReasonLocalPolicyDenied, reason)
	})

	t.Run("非官方客户端仍先命中 codex_cli_only 的 403", func(t *testing.T) {
		rec, c := newCodexResponsesOnlyTestContext("/v1/chat/completions")
		svc := &OpenAIGatewayService{codexDetector: &stubCodexRestrictionDetector{result: CodexClientRestrictionDetectionResult{
			Enabled: true, Matched: false, Reason: CodexClientRestrictionReasonNotMatchedUA,
		}}}

		_, err := svc.ForwardAsChatCompletions(context.Background(), c, codexResponsesOnlyTestAccount(), body, "", "")

		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, rec.Code)
	})
}

// /v1/messages 入口：同样拒绝，错误体用 Anthropic 形态。
func TestOpenAIGatewayService_ForwardAsAnthropic_CodexResponsesOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec, c := newCodexResponsesOnlyTestContext("/v1/messages")
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-6.1-sol","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, codexResponsesOnlyTestAccount(), body, "", "")

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "error", gjson.Get(rec.Body.String(), "type").String())
	require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
	require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "/v1/responses")
}
