package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// injectPromptCacheKeyIfAbsent sets "prompt_cache_key" on a serialized
// Responses-API request body unless a non-blank string value is already
// present. Extracted from the API-key branch of forwardChatCompletions so the
// injection can be exercised (and its allocation cost measured) in isolation.
// Implemented with gjson/sjson instead of a map[string]any round-trip: setting
// one top-level field must not materialize the whole document as a generic
// object graph (~6x the body in allocations).
func injectPromptCacheKeyIfAbsent(body []byte, key string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, errors.New("unmarshal for prompt cache key injection: invalid JSON")
	}
	if existing := gjson.GetBytes(body, "prompt_cache_key"); existing.Type == gjson.String && strings.TrimSpace(existing.String()) != "" {
		return body, nil
	}
	out, err := sjson.SetBytes(body, "prompt_cache_key", key)
	if err != nil {
		return nil, fmt.Errorf("remarshal after prompt cache key injection: %w", err)
	}
	return out, nil
}
