package service

import (
	"testing"

	"github.com/tidwall/gjson"
)

// ccWebSearchBody 模拟 Claude Code 请求：多个 custom 工具 + WebSearch server tool。
func ccWebSearchBody() []byte {
	return []byte(`{
		"model": "glm-5.3",
		"max_tokens": 1024,
		"tools": [
			{"name": "Bash", "type": "custom", "input_schema": {"type": "object"}},
			{"name": "web_search", "type": "web_search_20250305", "max_uses": 5},
			{"name": "Read", "type": "custom", "input_schema": {"type": "object"}}
		],
		"messages": [{"role": "user", "content": "hi"}]
	}`)
}

func TestStripWebSearchTools_RemovesServerToolOnly(t *testing.T) {
	out, stripped := StripWebSearchTools(ccWebSearchBody())
	if len(stripped) != 1 || stripped[0] != "web_search_20250305" {
		t.Fatalf("stripped = %v, want [web_search_20250305]", stripped)
	}
	names := gjson.GetBytes(out, "tools.#.name").Array()
	if len(names) != 2 || names[0].String() != "Bash" || names[1].String() != "Read" {
		t.Fatalf("remaining tools = %v", names)
	}
}

func TestStripWebSearchTools_EmptyToolsRemovesField(t *testing.T) {
	body := []byte(`{"model":"glm-5.3","tools":[{"type":"web_search_20250305"}]}`)
	out, stripped := StripWebSearchTools(body)
	if len(stripped) != 1 {
		t.Fatalf("stripped = %v, want 1", stripped)
	}
	if gjson.GetBytes(out, "tools").Exists() {
		t.Fatalf("empty tools should be removed, got %s", gjson.GetBytes(out, "tools").Raw)
	}
}

func TestStripWebSearchTools_ToolChoiceFallsBackToAuto(t *testing.T) {
	body := []byte(`{
		"model": "glm-5.3",
		"tools": [{"name": "web_search", "type": "web_search_20250305"}, {"name": "Bash", "type": "custom"}],
		"tool_choice": {"type": "tool", "name": "web_search"}
	}`)
	out, _ := StripWebSearchTools(body)
	if got := gjson.GetBytes(out, "tool_choice.type").String(); got != "auto" {
		t.Fatalf("tool_choice.type = %q, want auto", got)
	}
}

func TestStripWebSearchTools_ToolChoiceUnrelatedUntouched(t *testing.T) {
	body := []byte(`{
		"model": "glm-5.3",
		"tools": [{"name": "web_search", "type": "web_search_20250305"}, {"name": "Bash", "type": "custom"}],
		"tool_choice": {"type": "tool", "name": "Bash"}
	}`)
	out, _ := StripWebSearchTools(body)
	if got := gjson.GetBytes(out, "tool_choice.name").String(); got != "Bash" {
		t.Fatalf("tool_choice.name = %q, want Bash (untouched)", got)
	}
}

func TestStripWebSearchTools_CustomToolNamedWebSearchUntouched(t *testing.T) {
	// 只按 type 判定：用户自定义同名 custom 工具不剥
	body := []byte(`{"model":"glm-5.3","tools":[{"name":"web_search","type":"custom"}]}`)
	out, stripped := StripWebSearchTools(body)
	if stripped != nil {
		t.Fatalf("stripped = %v, want nil", stripped)
	}
	if gjson.GetBytes(out, "tools.#").Int() != 1 {
		t.Fatal("tools should be untouched")
	}
}

func TestStripWebSearchTools_NoTools(t *testing.T) {
	body := []byte(`{"model":"glm-5.3","messages":[]}`)
	out, stripped := StripWebSearchTools(body)
	if stripped != nil || string(out) != string(body) {
		t.Fatal("unexpected mutation")
	}
}

func TestIsVolcCodingPlanAccount(t *testing.T) {
	// 直读凭据 base_url，与账户 type 无关（upstream 型 coding 账户也要剥）
	coding := &Account{Credentials: map[string]any{"base_url": "https://ark.cn-beijing.volces.com/api/coding"}}
	if !isVolcCodingPlanAccount(coding) {
		t.Fatal("coding endpoint should match")
	}
	upstreamCoding := &Account{Type: AccountTypeUpstream, Credentials: map[string]any{"base_url": "https://ark.cn-beijing.volces.com/api/coding"}}
	if !isVolcCodingPlanAccount(upstreamCoding) {
		t.Fatal("upstream-type coding account should match")
	}
	agent := &Account{Credentials: map[string]any{"base_url": "https://ark.cn-beijing.volces.com/api/agent"}}
	if isVolcCodingPlanAccount(agent) {
		t.Fatal("agent endpoint should not match")
	}
	// 其他厂商的 coding 端点即使路径相同也不能剥（其套餐支持 web_search）
	otherVendor := &Account{Credentials: map[string]any{"base_url": "https://api.other-vendor.example.com/api/coding"}}
	if isVolcCodingPlanAccount(otherVendor) {
		t.Fatal("non-volc /api/coding endpoint should not match")
	}
	zhipu := &Account{Credentials: map[string]any{"base_url": "https://open.bigmodel.cn/api/anthropic"}}
	if isVolcCodingPlanAccount(zhipu) {
		t.Fatal("zhipu endpoint should not match")
	}
	if isVolcCodingPlanAccount(&Account{}) {
		t.Fatal("empty account should not match")
	}
}
