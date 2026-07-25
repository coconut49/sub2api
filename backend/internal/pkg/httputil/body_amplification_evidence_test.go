package httputil

// Fork evidence test (coconut49/sub2api): quantifies request-body read
// amplification in ReadRequestBodyWithPrealloc.
//
// Measured reality (Go 1.26): with Content-Length known the read is efficient
// (~1.1x, the 1 MiB prealloc cap does NOT cause meaningful doubling — the
// static-analysis claim of ~2x was wrong). Without Content-Length (chunked
// uploads, and HTTP/2 clients that omit the header — this gateway enables h2c
// by default), the buffer grows from 512 B and cumulative allocation measures
// ~4x the body size.
//
// The known-length subtest is a regression guard; the chunked subtest is the
// evidence of real amplification. If the chunked assertion starts failing,
// upstream fixed the growth path — retire that half and any fork patch on it.

import (
	"bytes"
	"io"
	"net/http/httptest"
	"runtime"
	"testing"
)

// measureAllocBytes returns cumulative heap bytes allocated while running fn.
// Runs on a locked goroutine with GC pinned before/after to keep the numbers
// stable; suitable for MB-scale assertions, not byte-exact ones.
func measureAllocBytes(t *testing.T, fn func()) uint64 {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func newBodyPayload(size int) []byte {
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = 'a' + byte(i%26)
	}
	return payload
}

// chunkedReader hides the underlying reader's length so httptest.NewRequest
// records ContentLength == -1, mimicking a chunked upload.
type chunkedReader struct{ r io.Reader }

func (c *chunkedReader) Read(p []byte) (int, error) { return c.r.Read(p) }

func TestEvidence_ReadRequestBody_AllocAmplification(t *testing.T) {
	const bodySize = 8 << 20 // 8 MiB, > requestBodyReadMaxInitCap
	payload := newBodyPayload(bodySize)

	cases := []struct {
		name          string
		contentLength bool
		minRatio      float64 // amplification proven present (evidence)
		maxRatio      float64 // efficiency proven present (regression guard)
	}{
		{name: "content_length_known", contentLength: true, maxRatio: 1.5},
		{name: "chunked_unknown_length", contentLength: false, minRatio: 2.5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			allocated := measureAllocBytes(t, func() {
				var src io.Reader = bytes.NewReader(payload)
				if !tc.contentLength {
					src = &chunkedReader{r: src}
				}
				req := httptest.NewRequest("POST", "/v1/messages", src)
				var err error
				body, err = ReadRequestBodyWithPrealloc(req)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
			})
			if len(body) != bodySize {
				t.Fatalf("body size = %d, want %d", len(body), bodySize)
			}

			ratio := float64(allocated) / float64(bodySize)
			t.Logf("body=%d MiB  allocated=%.1f MiB  amplification=%.2fx",
				bodySize>>20, float64(allocated)/(1<<20), ratio)

			if tc.maxRatio > 0 && ratio > tc.maxRatio {
				t.Fatalf("amplification=%.2fx > %.1fx: known-length read regressed", ratio, tc.maxRatio)
			}
			if tc.minRatio > 0 && ratio < tc.minRatio {
				t.Fatalf("amplification=%.2fx < %.1fx: chunked-read growth appears fixed; retire this evidence", ratio, tc.minRatio)
			}
			runtime.KeepAlive(body)
		})
	}
}
