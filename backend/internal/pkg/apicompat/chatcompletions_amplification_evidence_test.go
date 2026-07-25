package apicompat

// Fork evidence test (coconut49/sub2api): quantifies the memory amplification
// of the /v1/chat/completions -> Responses transform chain as executed by
// service/openai_gateway_chat_completions.go (Unmarshal -> convert -> Marshal
// -> generic map round-trip for prompt_cache_key injection). All intermediate
// graphs stay live simultaneously in the real handler, so this test measures
// both cumulative allocation (GC churn) and simultaneously-live bytes.
// If the live amplification drops below the asserted floor, upstream reworked
// the chain — retire this evidence file and any fork patch built on it.

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func buildChatCompletionsBody(messages, bytesPerMessage int) []byte {
	chunk := strings.Repeat("The quick brown fox jumps over the lazy dog. ", bytesPerMessage/45+1)[:bytesPerMessage]
	var b strings.Builder
	_, _ = b.WriteString(`{"model":"gpt-5.2","stream":true,"messages":[`)
	for i := 0; i < messages; i++ {
		if i > 0 {
			_, _ = b.WriteString(",")
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		fmt.Fprintf(&b, `{"role":%q,"content":%q}`, role, fmt.Sprintf("msg %d: %s", i, chunk))
	}
	_, _ = b.WriteString(`]}`)
	return []byte(b.String())
}

func TestEvidence_ChatCompletionsTransform_AllocAmplification(t *testing.T) {
	body := buildChatCompletionsBody(40, 5*1024) // ~200 KiB request
	bodySize := len(body)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// Mirror of openai_gateway_chat_completions.go forwardChatCompletions:
	var chatReq ChatCompletionsRequest // :98
	if err := json.Unmarshal(body, &chatReq); err != nil {
		t.Fatalf("unmarshal chat request: %v", err)
	}
	responsesReq, err := ChatCompletionsToResponses(&chatReq) // :166
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	responsesBody, err := json.Marshal(responsesReq) // :172
	if err != nil {
		t.Fatalf("marshal responses request: %v", err)
	}
	var reqBody map[string]any // :194 (OAuth/codex path)
	if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	reqBody["prompt_cache_key"] = "sess_evidence"
	finalBody, err := json.Marshal(reqBody) // :214
	if err != nil {
		t.Fatalf("marshal final: %v", err)
	}

	var afterChurn runtime.MemStats
	runtime.ReadMemStats(&afterChurn)
	churn := afterChurn.TotalAlloc - before.TotalAlloc

	// Everything above is still reachable here, exactly as in the handler
	// (chatReq/responsesReq are used again after forwarding for usage fields).
	runtime.GC()
	var afterLive runtime.MemStats
	runtime.ReadMemStats(&afterLive)
	live := afterLive.HeapAlloc - before.HeapAlloc

	churnRatio := float64(churn) / float64(bodySize)
	liveRatio := float64(live) / float64(bodySize)
	t.Logf("body=%d KiB  churn=%.1f KiB (%.1fx)  live-simultaneous=%.1f KiB (%.1fx)",
		bodySize>>10, float64(churn)/1024, churnRatio, float64(live)/1024, liveRatio)

	if len(finalBody) == 0 {
		t.Fatal("empty final body")
	}
	// One typed graph + one Responses graph + generic map + 3 serialized
	// copies, all live at once. Today this measures well above 4x live and
	// ~8x+ churn. A right-sized pipeline would be ~2x live.
	if liveRatio < 3.0 {
		t.Fatalf("live amplification=%.2fx < 3x: transform chain appears fixed; retire this evidence test", liveRatio)
	}
	runtime.KeepAlive(body)
	runtime.KeepAlive(chatReq)
	runtime.KeepAlive(responsesReq)
	runtime.KeepAlive(responsesBody)
	runtime.KeepAlive(reqBody)
	runtime.KeepAlive(finalBody)
}
