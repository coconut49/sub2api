package service

import (
	"unsafe"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/tidwall/gjson"
)

// thinkingBlocksNeedFiltering 低成本预检 filterThinkingBlocksInternal 是否会
// 剔除至少一个块：用 gjson 惰性遍历代替整个 body 的泛型图 unmarshal。开启
// extended thinking 的常态请求（历史 thinking block 签名齐全）在这里直接短路，
// 免付全量解析的分配代价。
//
// 判定逻辑与 filterThinkingBlocksInternal 的剔除条件逐条对应：
//   - thinking/redacted_thinking 块仅在「顶层 thinking 为 enabled/adaptive 且
//     role 为 assistant 且 signature 为非空、非 dummy 字符串」时保留，其余剔除；
//   - 无 type 但含 "thinking" 键的块剔除；
//   - content 非数组、消息非对象等情况与完整路径一样不做处理。
//
// 已知偏差：JSON 重复键时 gjson 取首个、encoding/json 取末个。body 在入口已经
// 过校验且由正规客户端生成，不含重复键；即使出现，后果也只是退回完整路径或
// 保持原样，不产生新行为。
func thinkingBlocksNeedFiltering(body []byte) bool {
	// 与 FilterThinkingBlocksForRetry 相同的零拷贝模式：GetBytes/ParseBytes 都会
	// 把匹配子树复制成安全字符串（messages 子树 ≈ 整个 body，一次全量拷贝），
	// unsafe 别名 + Get 则完全不拷贝。仅在本函数内只读使用，不逃逸保存。
	jsonStr := *(*string)(unsafe.Pointer(&body))

	thinkingEnabled := false
	if t := gjson.Get(jsonStr, "thinking.type"); t.Type == gjson.String {
		thinkingEnabled = t.Str == "enabled" || t.Str == "adaptive"
	}

	messages := gjson.Get(jsonStr, "messages")
	if !messages.IsArray() {
		return false
	}

	need := false
	messages.ForEach(func(_, msg gjson.Result) bool {
		if !msg.IsObject() {
			return true
		}
		role := msg.Get("role").String()
		content := msg.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(_, block gjson.Result) bool {
			if !block.IsObject() {
				return true
			}
			blockType := block.Get("type")
			switch blockType.String() {
			case "thinking", "redacted_thinking":
				if thinkingEnabled && role == "assistant" {
					if sig := block.Get("signature"); sig.Type == gjson.String &&
						sig.Str != "" && sig.Str != antigravity.DummyThoughtSignature {
						return true // 保留，继续扫
					}
				}
				need = true
				return false
			case "":
				if block.Get("thinking").Exists() {
					need = true
					return false
				}
			}
			return true
		})
		return !need
	})
	return need
}
