package service

// Fork evidence test (coconut49/sub2api): quantifies the per-step memory cost
// of the /v1/messages body-rewrite chain executed by GatewayService.Forward
// (gateway_forward.go:139-349). Two realistic scenarios:
//
//   - mimicry: OAuth account + third-party (non-Claude-Code) client — the
//     full rewrite chain fires.
//   - claude_code_client: genuine Claude Code client — mimicry is skipped and
//     only the shared pre-filter steps run.
//
// The mirror calls the same package-level rewrite functions in the same order
// as Forward, routed through a real ParsedRequest.ReplaceBody so the
// re-validate + re-parse cost of every replaceBody() call is included.
// Receiver/settings-gated steps that need a wired GatewayService
// (rewriteMessageCacheControlIfEnabled, normalizeClientDatelineIfEnabled,
// injectAnthropicCacheControlTTL1h gating, buildUpstreamRequest rewrites) are
// noted but not mirrored; measured numbers are therefore a LOWER bound.
//
// If the mimicry amplification floor assertion starts failing, upstream
// reworked the chain — retire this evidence file and any fork patch on it.

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

func buildClaudeCodeStyleBody(messages, bytesPerMessage int) []byte {
	text := strings.Repeat("Refactor the scheduler so retries respect the budget. ", bytesPerMessage/54+1)[:bytesPerMessage]
	var b strings.Builder
	// 真实 Claude Code 回放 thinking 历史时必带顶层 thinking 开关；历史块签名齐全。
	b.WriteString(`{"model":"claude-sonnet-5","max_tokens":16384,"stream":true,"thinking":{"type":"enabled","budget_tokens":8192},`)
	b.WriteString(`"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."},`)
	b.WriteString(`{"type":"text","text":"` + strings.Repeat("Project context and instructions. ", 60) + `","cache_control":{"type":"ephemeral"}}],`)
	b.WriteString(`"tools":[`)
	for i := 0; i < 8; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"name":"tool_%d","description":"does thing %d","input_schema":{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path"]}}`, i, i)
	}
	b.WriteString(`],"metadata":{"user_id":"user_abc123"},"messages":[`)
	for i := 0; i < messages; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		switch {
		case i%4 == 1: // assistant turn with tool_use
			fmt.Fprintf(&b, `{"role":"assistant","content":[{"type":"text","text":"turn %d: %s"},{"type":"tool_use","id":"toolu_%d","name":"tool_1","input":{"path":"a.go"}}]}`, i, text[:200], i)
		case i%4 == 2: // user turn with tool_result
			fmt.Fprintf(&b, `{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%d","content":[{"type":"text","text":"turn %d: %s"}]}]}`, i-1, i, text)
		case i%4 == 3: // assistant turn with a signed thinking block
			fmt.Fprintf(&b, `{"role":"assistant","content":[{"type":"thinking","thinking":"turn %d: %s","signature":"sig_%d"},{"type":"text","text":"ok"}]}`, i, text[:300], i)
		default:
			fmt.Fprintf(&b, `{"role":"user","content":[{"type":"text","text":"turn %d: %s"}]}`, i, text)
		}
	}
	// Trailing cache_control breakpoints like a real Claude Code incremental turn.
	b.WriteString(`,{"role":"user","content":[{"type":"text","text":"continue","cache_control":{"type":"ephemeral"}}]}]}`)
	return []byte(b.String())
}

type chainStep struct {
	name string
	run  func(body []byte) []byte
}

func runChainWithPerStepAllocs(t *testing.T, body []byte, steps []chainStep) (totalChurn uint64) {
	t.Helper()
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), domain.PlatformAnthropic)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	current := parsed.Body.Bytes()
	var stats runtime.MemStats
	for _, step := range steps {
		runtime.GC()
		runtime.ReadMemStats(&stats)
		before := stats.TotalAlloc
		next := step.run(current)
		if err := parsed.ReplaceBody(next); err != nil {
			t.Fatalf("step %s: replace body: %v", step.name, err)
		}
		current = parsed.Body.Bytes()
		runtime.ReadMemStats(&stats)
		delta := stats.TotalAlloc - before
		totalChurn += delta
		t.Logf("  step %-28s churn=%8.1f KiB (%.2fx body)", step.name, float64(delta)/1024, float64(delta)/float64(len(body)))
	}
	return totalChurn
}

func TestEvidence_MessagesRewriteChain_AllocAmplification(t *testing.T) {
	body := buildClaudeCodeStyleBody(40, 5*1024) // ~220 KiB request
	bodySize := len(body)
	svc := &GatewayService{}
	const mappedModel = "claude-sonnet-5-20250929"

	preFilterSteps := []chainStep{
		{"enforceCacheControlLimit", enforceCacheControlLimit},
		{"ReplaceModelInBody", func(b []byte) []byte { return svc.replaceModelInBody(b, mappedModel) }},
		{"StripEmptyTextBlocks", StripEmptyTextBlocks},
		{"FilterWebSearchHistoryBlocks", func(b []byte) []byte { return FilterWebSearchHistoryBlocks(b, mappedModel) }},
		{"FilterThinkingBlocks", func(b []byte) []byte { return FilterThinkingBlocks(b, mappedModel) }},
	}

	t.Run("claude_code_client", func(t *testing.T) {
		// Genuine Claude Code client: mimicry skipped (gateway_forward.go:172-173),
		// only shared pre-filters run.
		churn := runChainWithPerStepAllocs(t, body, preFilterSteps)
		t.Logf("TOTAL body=%d KiB churn=%.1f KiB (%.2fx)", bodySize>>10, float64(churn)/1024, float64(churn)/float64(bodySize))
	})

	t.Run("mimicry_third_party_client", func(t *testing.T) {
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), domain.PlatformAnthropic)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		systemRaw, _ := parsed.SystemValue()
		mimicrySteps := append([]chainStep{
			{"rewriteSystemPromptBlocks", func(b []byte) []byte {
				return rewriteSystemForNonClaudeCodeWithPromptBlocks(b, systemRaw, "", "")
			}},
			{"normalizeClaudeOAuthBody", func(b []byte) []byte {
				next, _ := normalizeClaudeOAuthRequestBody(b, "claude-sonnet-5", claudeOAuthNormalizeOptions{})
				return next
			}},
			{"applyToolsLastCacheBreakpoint", func(b []byte) []byte {
				if rw := buildToolNameRewriteFromBody(b); rw != nil {
					return applyToolNameRewriteToBody(b, rw)
				}
				return applyToolsLastCacheBreakpoint(b)
			}},
		}, preFilterSteps...)
		mimicrySteps = append(mimicrySteps, chainStep{
			"injectCacheControlTTL1h", injectAnthropicCacheControlTTL1h,
		})

		churn := runChainWithPerStepAllocs(t, body, mimicrySteps)
		ratio := float64(churn) / float64(bodySize)
		t.Logf("TOTAL body=%d KiB churn=%.1f KiB (%.2fx)", bodySize>>10, float64(churn)/1024, ratio)

		// The mimicry chain re-allocates the body on every mutating step plus a
		// full re-validate/re-parse per replaceBody. Floor set conservatively;
		// if this fails, the chain got cheaper — retire this evidence.
		if ratio < 3.0 {
			t.Fatalf("mimicry chain churn=%.2fx < 3x: rewrite chain appears fixed; retire this evidence test", ratio)
		}
	})
}
