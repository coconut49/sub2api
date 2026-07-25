package service

// Fork tests (coconut49/sub2api) for FilterThinkingBlocks — Ticket 3.
//
// 当前行为：只要请求包含 thinking 相关字节（开启 extended thinking 的请求必然
// 包含），FilterThinkingBlocks 就把整个 body unmarshal 成 map[string]any 泛型图
// 来检查历史消息里的 thinking block 签名——即使所有签名都合法、无需剔除任何块
// （常态流量），也要付 ~6.4x body 的分配代价。
//
// 预期行为：先低成本预检"是否存在需要剔除的块"，没有就原样返回；只有真的需要
// 剔除时才走完整解析。剔除语义完全不变。
//
// Behavior 测试锁定现有剔除语义（修复前后都必须通过）；AllocCeiling 测试是
// TDD 驱动器（泛型图实现红，预检实现绿）。

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/tidwall/gjson"
)

const strictModel = "claude-sonnet-5-20250929"

func countThinkingBlocks(body []byte) int {
	count := 0
	gjson.GetBytes(body, "messages").ForEach(func(_, msg gjson.Result) bool {
		msg.Get("content").ForEach(func(_, block gjson.Result) bool {
			t := block.Get("type").String()
			if t == "thinking" || t == "redacted_thinking" {
				count++
			}
			if !block.Get("type").Exists() && block.Get("thinking").Exists() {
				count++
			}
			return true
		})
		return true
	})
	return count
}

func TestFilterThinkingBlocks_Behavior(t *testing.T) {
	const validSig = "EqQBCgIYAhIkValidSignaturePayload"
	cases := []struct {
		name       string
		model      string
		body       string
		wantBlocks int  // thinking blocks remaining after filter
		wantSame   bool // body must be returned unchanged
	}{
		{
			name:  "valid_signatures_kept_body_unchanged",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","thinking":{"type":"enabled","budget_tokens":4096},"messages":[
				{"role":"user","content":[{"type":"text","text":"hi"}]},
				{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning...","signature":"` + validSig + `"},{"type":"text","text":"answer"}]}]}`,
			wantBlocks: 1,
			wantSame:   true,
		},
		{
			name:  "missing_signature_removed",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","thinking":{"type":"enabled"},"messages":[
				{"role":"assistant","content":[{"type":"thinking","thinking":"no sig"},{"type":"text","text":"answer"}]}]}`,
			wantBlocks: 0,
		},
		{
			name:  "dummy_signature_removed",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","thinking":{"type":"enabled"},"messages":[
				{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"` + antigravity.DummyThoughtSignature + `"},{"type":"text","text":"a"}]}]}`,
			wantBlocks: 0,
		},
		{
			name:  "thinking_disabled_removes_even_signed_blocks",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","messages":[
				{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"` + validSig + `"},{"type":"text","text":"a"}]}]}`,
			wantBlocks: 0,
		},
		{
			name:  "non_assistant_thinking_removed",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","thinking":{"type":"enabled"},"messages":[
				{"role":"user","content":[{"type":"thinking","thinking":"x","signature":"` + validSig + `"},{"type":"text","text":"q"}]}]}`,
			wantBlocks: 0,
		},
		{
			name:  "redacted_thinking_without_signature_removed",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","thinking":{"type":"enabled"},"messages":[
				{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque"},{"type":"text","text":"a"}]}]}`,
			wantBlocks: 0,
		},
		{
			name:  "typeless_block_with_thinking_key_removed",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","thinking":{"type":"enabled"},"messages":[
				{"role":"assistant","content":[{"thinking":"stray"},{"type":"text","text":"a"}]}]}`,
			wantBlocks: 0,
		},
		{
			name:  "string_content_untouched",
			model: strictModel,
			body: `{"model":"claude-sonnet-5","thinking":{"type":"enabled"},"messages":[
				{"role":"assistant","content":"plain string mentioning \"thinking\": here"}]}`,
			wantBlocks: 0,
			wantSame:   true,
		},
		{
			name:  "non_strict_model_untouched",
			model: "gpt-5.2",
			body: `{"model":"gpt-5.2","thinking":{"type":"enabled"},"messages":[
				{"role":"assistant","content":[{"type":"thinking","thinking":"no sig"}]}]}`,
			wantBlocks: 1,
			wantSame:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := []byte(tc.body)
			out := FilterThinkingBlocks(in, tc.model)
			if got := countThinkingBlocks(out); got != tc.wantBlocks {
				t.Fatalf("thinking blocks after filter = %d, want %d\nout: %s", got, tc.wantBlocks, out)
			}
			if tc.wantSame && string(out) != string(in) {
				t.Fatalf("body changed but should be untouched:\nin:  %s\nout: %s", in, out)
			}
			if !gjson.ValidBytes(out) {
				t.Fatalf("output is not valid JSON: %s", out)
			}
		})
	}
}

func buildValidSignedThinkingBody(messages, bytesPerMessage int) []byte {
	text := strings.Repeat("Consider the tradeoffs before answering the question. ", bytesPerMessage/54+1)[:bytesPerMessage]
	var b strings.Builder
	b.WriteString(`{"model":"claude-sonnet-5","max_tokens":16384,"thinking":{"type":"enabled","budget_tokens":8192},"messages":[`)
	for i := 0; i < messages; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		if i%2 == 1 {
			fmt.Fprintf(&b, `{"role":"assistant","content":[{"type":"thinking","thinking":"turn %d: %s","signature":"EqQBCgIYAhIkSig%d"},{"type":"text","text":"turn %d answer"}]}`, i, text[:400], i, i)
		} else {
			fmt.Fprintf(&b, `{"role":"user","content":[{"type":"text","text":"turn %d: %s"}]}`, i, text)
		}
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func TestFilterThinkingBlocks_AllocCeiling_NothingToFilter(t *testing.T) {
	body := buildValidSignedThinkingBody(40, 5*1024) // ~110 KiB, all signatures valid
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out := FilterThinkingBlocks(body, strictModel)
	runtime.ReadMemStats(&after)
	churn := after.TotalAlloc - before.TotalAlloc
	ratio := float64(churn) / float64(len(body))
	t.Logf("body=%d KiB  churn=%.1f KiB (%.2fx)", len(body)>>10, float64(churn)/1024, ratio)

	if string(out) != string(body) {
		t.Fatal("nothing should be filtered from an all-valid-signature body")
	}
	// 无需剔除时应只付轻量预检的代价(接近 0x);泛型图实现约 6.4x。
	if ratio > 0.5 {
		t.Fatalf("no-op filter churn=%.2fx body > 0.5x ceiling: generic-graph parse on the common path is back", ratio)
	}
	runtime.KeepAlive(out)
}
