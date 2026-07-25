package handler

// Fork evidence test (coconut49/sub2api): proves that evaluating
// clientRequestedUsageFields(c, ...) AFTER the handler returns reads data
// belonging to a DIFFERENT request, because gin recycles *gin.Context through
// a sync.Pool and rebinds c.Request on reuse.
//
// This is the failure mechanism behind the async usage-record closures at
// gateway_handler.go (Messages, both the non-retry and retry paths) and
// openai_chat_completions.go: the closure runs on a worker pool after the
// handler frame is gone, so its clientRequestedUsageFields(c, ...) call can
// pick up the next request's RequestedPublicModel — corrupting billing
// attribution — and races with gin's context rebinding under load.
//
// The deferred_evaluation subtest asserts the WRONG value is observed (the
// bug mechanism is real); the eager_evaluation subtest asserts the hoisted
// pattern used by the fork fix is immune. If deferred_evaluation starts
// failing, gin's pooling semantics changed and the fork patch can be retired.

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func serveWithPublicModel(engine *gin.Engine, model string) {
	req := httptest.NewRequest("POST", "/t", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.RequestedPublicModel, model))
	engine.ServeHTTP(httptest.NewRecorder(), req)
}

func TestEvidence_UsageFieldsAfterHandlerReturn_ContextReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("deferred_evaluation_reads_other_request", func(t *testing.T) {
		engine := gin.New()
		var deferred func() service.ChannelUsageFields
		engine.POST("/t", func(c *gin.Context) {
			// Same shape as the buggy sites: the closure captures c and calls
			// clientRequestedUsageFields when it eventually runs on the worker.
			deferred = func() service.ChannelUsageFields {
				return clientRequestedUsageFields(c, service.ChannelMappingResult{}, "fallback", "up")
			}
		})

		serveWithPublicModel(engine, "claude-model-a")
		// Handler for request A has returned; gin put its context back into the
		// pool. Request B now reuses the same *gin.Context object.
		serveWithPublicModel(engine, "gpt-model-b")

		got := deferred()
		// The deferred closure belongs to request A but observes request B's
		// model: billing attribution crosses requests.
		if got.OriginalModel != "gpt-model-b" {
			t.Fatalf("deferred evaluation observed %q; gin context reuse no longer leaks across requests — retire this evidence test and the fork patch", got.OriginalModel)
		}
	})

	t.Run("eager_evaluation_is_safe", func(t *testing.T) {
		engine := gin.New()
		var perRequest []service.ChannelUsageFields
		engine.POST("/t", func(c *gin.Context) {
			// The fixed pattern: evaluate on the handler stack, capture the value.
			perRequest = append(perRequest, clientRequestedUsageFields(c, service.ChannelMappingResult{}, "fallback", "up"))
		})

		serveWithPublicModel(engine, "claude-model-a")
		serveWithPublicModel(engine, "gpt-model-b")

		if perRequest[0].OriginalModel != "claude-model-a" || perRequest[1].OriginalModel != "gpt-model-b" {
			t.Fatalf("eager evaluation observed %q/%q, want claude-model-a/gpt-model-b",
				perRequest[0].OriginalModel, perRequest[1].OriginalModel)
		}
	})
}
