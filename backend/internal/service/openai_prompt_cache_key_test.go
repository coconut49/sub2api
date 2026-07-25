package service

// Fork tests (coconut49/sub2api) for injectPromptCacheKeyIfAbsent: behavior
// equivalence with the original map[string]any round-trip in
// forwardChatCompletions' API-key branch, plus an allocation ceiling. The
// ceiling is the TDD driver: the map round-trip implementation allocates a
// full generic object graph (~8x the body) just to set one field and fails
// the ceiling; a gjson/sjson implementation stays near 1x.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func buildResponsesShapeBody(items, bytesPerItem int) []byte {
	text := strings.Repeat("Summarize the diff and propose a commit message. ", bytesPerItem/49+1)[:bytesPerItem]
	var b strings.Builder
	b.WriteString(`{"model":"gpt-5.2","stream":true,"store":false,"input":[`)
	for i := 0; i < items; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"type":"message","role":"user","content":[{"type":"input_text","text":"item %d: %s"}]}`, i, text)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func TestInjectPromptCacheKeyIfAbsent_Behavior(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		key     string
		want    string // expected prompt_cache_key value after injection
		wantErr bool
	}{
		{name: "absent_injects", body: `{"model":"gpt-5.2","input":"hi"}`, key: "sess_a", want: "sess_a"},
		{name: "blank_string_injects", body: `{"model":"gpt-5.2","prompt_cache_key":"  ","input":"hi"}`, key: "sess_a", want: "sess_a"},
		{name: "existing_preserved", body: `{"model":"gpt-5.2","prompt_cache_key":"keep","input":"hi"}`, key: "sess_a", want: "keep"},
		{name: "non_string_overwritten", body: `{"model":"gpt-5.2","prompt_cache_key":42,"input":"hi"}`, key: "sess_a", want: "sess_a"},
		{name: "invalid_json_errors", body: `{"model":`, key: "sess_a", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := injectPromptCacheKeyIfAbsent([]byte(tc.body), tc.key)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("inject: %v", err)
			}
			if got := gjson.GetBytes(out, "prompt_cache_key").String(); got != tc.want {
				t.Fatalf("prompt_cache_key = %q, want %q", got, tc.want)
			}
			// The rest of the document must be semantically untouched.
			var wantDoc, gotDoc map[string]any
			if err := json.Unmarshal([]byte(tc.body), &wantDoc); err != nil {
				t.Fatalf("unmarshal input: %v", err)
			}
			if err := json.Unmarshal(out, &gotDoc); err != nil {
				t.Fatalf("unmarshal output: %v", err)
			}
			delete(wantDoc, "prompt_cache_key")
			delete(gotDoc, "prompt_cache_key")
			if !reflect.DeepEqual(wantDoc, gotDoc) {
				t.Fatalf("document mutated beyond prompt_cache_key:\nin:  %s\nout: %s", tc.body, out)
			}
		})
	}
}

func TestInjectPromptCacheKeyIfAbsent_AllocCeiling(t *testing.T) {
	body := buildResponsesShapeBody(40, 5*1024) // ~200 KiB
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out, err := injectPromptCacheKeyIfAbsent(body, "sess_alloc")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	runtime.ReadMemStats(&after)
	churn := after.TotalAlloc - before.TotalAlloc
	ratio := float64(churn) / float64(len(body))
	t.Logf("body=%d KiB  churn=%.1f KiB (%.2fx)", len(body)>>10, float64(churn)/1024, ratio)

	// Setting one top-level field should cost about one body copy. The
	// map[string]any round-trip costs ~8x and must not come back.
	if ratio > 2.0 {
		t.Fatalf("injection churn=%.2fx body > 2x ceiling: generic-graph round-trip is back", ratio)
	}
	runtime.KeepAlive(out)
}
