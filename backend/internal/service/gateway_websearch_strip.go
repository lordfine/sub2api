package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 火山 Coding Plan 端点特征（https://ark.cn-beijing.volces.com/api/coding）。
// 该套餐不支持 Anthropic server tool：带 web_search_20250305 工具定义的请求
// 会被上游 403，且 403 在 failover 名单会轮完全部 coding 账户并累积错误计数
// 把账户打入 status=error（2026-09-09 事故，acc31/acc34）。
// 按端点判断 = 按套餐能力判断，新接 coding 账户自动覆盖，无需逐账户配置。
const (
	volcDomainMarker         = "volces.com"
	volcCodingEndpointMarker = "/api/coding"
)

// isVolcCodingPlanAccount 判断账户是否走火山云 Coding Plan 端点。
// 直读凭据 base_url（不限账户类型：GetBaseURL 对非 apikey 型返回空，
// upstream 型 coding 账户会被漏掉）。双条件锁定火山云系：仅凭
// /api/coding 路径会误伤其他厂商将来同路径的 coding 端点——
// 其他厂商的 coding 套餐（智谱等）支持 web_search，不能剥。
func isVolcCodingPlanAccount(account *Account) bool {
	baseURL := account.GetCredential("base_url")
	return strings.Contains(baseURL, volcDomainMarker) && strings.Contains(baseURL, volcCodingEndpointMarker)
}

// StripWebSearchTools 从请求体 tools[] 剥掉 web_search 系 server tool 定义
// （type 以 "web_search" 开头，如 web_search_20250305；按 type 判定，
// 用户自定义同名 custom 工具不受影响）。剥除后模型无感降级（不做联网搜索），
// 客户端不感知。附带处理：
//  1. tools 剥空则整字段删除（空数组可能过不了上游校验）；
//  2. tool_choice 指向被剥工具（type 或 name 匹配）时回落 auto，避免上游 400。
//
// 返回 (改写后的 body, 被剥的 type 列表)；未命中返回 (原 body, nil)。
func StripWebSearchTools(body []byte) ([]byte, []string) {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() {
		return body, nil
	}
	arr := tools.Array()
	idxs := make([]int, 0, 1)
	stripped := make([]string, 0, 1)
	refs := make(map[string]struct{}, 2) // 被剥工具的 type 与 name，供 tool_choice 匹配
	for i, t := range arr {
		toolType := t.Get("type").String()
		if !strings.HasPrefix(toolType, toolTypeWebSearchPrefix) {
			continue
		}
		idxs = append(idxs, i)
		stripped = append(stripped, toolType)
		refs[toolType] = struct{}{}
		if name := t.Get("name").String(); name != "" {
			refs[name] = struct{}{}
		}
	}
	if len(idxs) == 0 {
		return body, nil
	}
	// 倒序删除，避免数组前移导致索引错位。任一步失败即整体放弃，
	// 返回原始 body（sjson 不原地修改，快照仍完整），宁可 403 不改坏请求体。
	orig := body
	for k := len(idxs) - 1; k >= 0; k-- {
		next, err := sjson.DeleteBytes(body, fmt.Sprintf("tools.%d", idxs[k]))
		if err != nil {
			return orig, nil
		}
		body = next
	}
	if gjson.GetBytes(body, "tools.#").Int() == 0 {
		if next, err := sjson.DeleteBytes(body, "tools"); err == nil {
			body = next
		}
	}
	if tc := gjson.GetBytes(body, "tool_choice"); tc.Get("type").String() == "tool" {
		if _, hit := refs[tc.Get("name").String()]; hit {
			if next, err := sjson.SetRawBytes(body, "tool_choice", []byte(`{"type":"auto"}`)); err == nil {
				body = next
			}
		}
	}
	return body, stripped
}
