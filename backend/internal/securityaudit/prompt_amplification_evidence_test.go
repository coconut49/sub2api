package securityaudit

// Fork evidence test (coconut49/sub2api): quantifies the per-request memory
// amplification of the prompt-audit pipeline on the gateway hot path:
// Request.Clone() copies the full body unconditionally, and
// ExtractPromptSnapshot unmarshals the entire body into a generic
// map[string]any/[]any graph (typically several times the JSON byte size),
// then builds scanText/metadataText string joins on top.
//
// Measured reality (Go 1.26): cumulative allocation (GC churn) is ~25x the
// body size, while simultaneously-live memory is modest (~1.4x — the generic
// graph is transient). Prompt audit is therefore a GC-pressure/throughput
// problem on the hot path, not a resident-memory problem, and only when the
// feature is enabled. If the churn assertion starts failing, upstream
// optimized the snapshot path — retire this evidence file and any fork patch
// built on it.

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func buildAnthropicMessagesBody(messages, bytesPerMessage int) []byte {
	chunk := strings.Repeat("Explain the design tradeoffs in this code path. ", bytesPerMessage/48+1)[:bytesPerMessage]
	var b strings.Builder
	_, _ = b.WriteString(`{"model":"claude-sonnet-5","max_tokens":8192,"system":"You are a helpful assistant.","messages":[`)
	for i := 0; i < messages; i++ {
		if i > 0 {
			_, _ = b.WriteString(",")
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		fmt.Fprintf(&b,
			`{"role":%q,"content":[{"type":"text","text":%q}]}`,
			role, fmt.Sprintf("turn %d: %s", i, chunk))
	}
	_, _ = b.WriteString(`]}`)
	return []byte(b.String())
}

func TestEvidence_PromptSnapshot_AllocAmplification(t *testing.T) {
	body := buildAnthropicMessagesBody(40, 5*1024) // ~200 KiB request
	bodySize := len(body)
	req := Request{
		RequestID: "req_evidence",
		UserID:    1,
		Protocol:  "messages",
		Model:     "claude-sonnet-5",
		Body:      body,
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// Mirror of coordinator.go Evaluate: clone for the worker, then snapshot.
	clone := req.Clone()
	snapshot, err := ExtractPromptSnapshot(clone)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	var afterChurn runtime.MemStats
	runtime.ReadMemStats(&afterChurn)
	churn := afterChurn.TotalAlloc - before.TotalAlloc

	// Clone + snapshot both stay reachable until the audit verdict, as in the
	// blocking-mode coordinator.
	runtime.GC()
	var afterLive runtime.MemStats
	runtime.ReadMemStats(&afterLive)
	live := afterLive.HeapAlloc - before.HeapAlloc

	churnRatio := float64(churn) / float64(bodySize)
	liveRatio := float64(live) / float64(bodySize)
	t.Logf("body=%d KiB  churn=%.1f KiB (%.1fx)  live-simultaneous=%.1f KiB (%.1fx)  scanText=%d KiB",
		bodySize>>10, float64(churn)/1024, churnRatio, float64(live)/1024, liveRatio, len(snapshot.ScanText)>>10)

	// Full body copy + generic JSON graph + segment strings + scanText +
	// metadataText: today this measures ~25x churn. A gjson-style streaming
	// extractor would be low-single-digit-x. Floor set conservatively at 8x.
	if churnRatio < 8.0 {
		t.Fatalf("churn amplification=%.2fx < 8x: snapshot path appears fixed; retire this evidence test", churnRatio)
	}
	runtime.KeepAlive(clone)
	runtime.KeepAlive(snapshot)
}
