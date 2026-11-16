package service

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// codexResponsesOnlyExtraKey 账号级开关：只接受原生 Responses 协议入站。
//
// 背景：chat/completions 与 /v1/messages 入站的请求会被网关重新拼装成 Responses
// 请求再发给上游，上游看到的不是 Codex 客户端自己构造的请求体。共享的 Codex OAuth
// 账号希望上游只看到原生请求，因此开启后在访问上游之前就地拒绝这两类入站，
// 并在错误消息里告诉客户端改走 /v1/responses。
const codexResponsesOnlyExtraKey = "codex_responses_only"

// CodexResponsesOnlyErrorCode 是 Responses-only 拒绝响应的错误码。
const CodexResponsesOnlyErrorCode = "responses_protocol_required"

// CodexResponsesOnlyMessage 是 Responses-only 拒绝响应的提示文案。
const CodexResponsesOnlyMessage = "This account only accepts the Responses API (/v1/responses). " +
	"该通道只接受 Responses 协议，请把客户端改为请求 /v1/responses。" +
	"magpie 用户：magpie provider set <id> responses=<网关地址>/v1 codex=as-sent，" +
	"再对所用模型执行 magpie model api <id>/<模型> responses。"

// IsCodexResponsesOnlyEnabled 返回 OpenAI OAuth 账号是否只接受原生 Responses 协议入站。
// 字段：accounts.extra.codex_responses_only。
// 字段缺失或类型不正确时，按 false（关闭）处理。
func (a *Account) IsCodexResponsesOnlyEnabled() bool {
	if a == nil || !a.IsOpenAIOAuth() || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra[codexResponsesOnlyExtraKey].(bool)
	return ok && enabled
}

// rejectNonResponsesInboundForCodexAccount 在账号开启 Responses-only 时写出拒绝响应并返回错误；
// 未开启时返回 nil，调用方照常转发。isAnthropic 决定错误体形态（/v1/messages 用 Anthropic 形态）。
func rejectNonResponsesInboundForCodexAccount(c *gin.Context, account *Account, isAnthropic bool) error {
	if !account.IsCodexResponsesOnlyEnabled() {
		return nil
	}
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
	if c != nil {
		if isAnthropic {
			c.JSON(http.StatusBadRequest, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "invalid_request_error",
					"message": CodexResponsesOnlyMessage,
				},
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"type":    "invalid_request_error",
					"code":    CodexResponsesOnlyErrorCode,
					"message": CodexResponsesOnlyMessage,
				},
			})
		}
	}
	return errors.New("codex_responses_only restriction: only the Responses API is accepted")
}
