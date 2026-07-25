# Fork Patch 清单

`main` 上位于上游 base tag 之后的每个 commit 在此登记一个条目。新增、修改、删除 patch 时同步更新本文件。

## fork-infra — fork 基础设施

- 用途：fork 的自我描述与自动化。CLAUDE.md、`.fork/`、`fork-sync.yml`、`fork-image.yml`。
- 退役条件：无，fork 存续期间永久保留。

## opus-5 — 提前采纳上游 claude-opus-5 适配

- 用途：网关支持 `claude-opus-5`。上游已在 release 后的 commit `6c9b84cc7`（feat: 适配 Anthropic 新模型 claude-opus-5）实现，本 patch 是该 commit 的原样 cherry-pick，diff 与上游逐字节一致。除模型登记外，它还修复定价兜底 3 倍超收与 Bedrock 版本闸门降级两个静默 bug。
- 涉及：以上游 commit 为准（backend constants / bedrock_request / billing_service / pricing_service / pricing JSON / 前端白名单与 scope 简称，含回归测试 `claude_opus5_test.go`）。pricing JSON 的改动是上游自己的内容，不违反「不本地修改该文件」规则。
- 退役条件：rebase 到包含 `6c9b84cc7` 的上游 release tag 时，git 因 patch-id 相同自动丢弃本 commit；届时删除本条目即可。

## perf-evidence — 请求路径内存放大证据测试

- 用途：用可复现的 alloc 测量测试锁定网关热路径的内存放大事实，作为后续优化 patch 的 TDD 基线与"上游已修复"的退役哨兵。实测(Go 1.26)：无 Content-Length 的请求体读取 churn 4.0×(带 Content-Length 仅 1.13×，无问题)；CC→Responses 转换链 churn 15.7×、同时存活 7.2×；prompt 审计快照 churn 25.3×(存活仅 1.2×，属 GC 压力而非驻留内存)。
- 涉及（全部新增文件，零上游冲突面）：
  - `backend/internal/pkg/httputil/body_amplification_evidence_test.go`
  - `backend/internal/pkg/apicompat/chatcompletions_amplification_evidence_test.go`
  - `backend/internal/securityaudit/prompt_amplification_evidence_test.go`
  - `backend/internal/service/gateway_forward_chain_amplification_evidence_test.go`：/v1/messages 改写链逐步测量。实测：Claude Code 客户端路径总 churn 8.5×，其中 FilterThinkingBlocks 单步 6.4×（无条件泛型图 unmarshal，无 fast path；对比 StripEmptyTextBlocks/FilterWebSearchHistoryBlocks 均为 0）；第三方客户端 mimicry 路径总 churn 49.3×（applyToolsLastCacheBreakpoint 单步 28.3×）。这两个数字是后续优化 patch 的候选靶点。
- 背景：上游 issue #4365(v0.1.156 内存暴涨，Codex 长上下文场景)、#1465(上游账号异常时内存占用高)均未解决；上游无相关修复 PR。
- 退役条件：某个测试的"amplification appears fixed"断言开始失败，即说明上游已优化对应路径，删除该测试文件并退役依赖它的优化 patch。

## fix-usage-ctx-capture — 异步计费闭包不再延迟读 gin.Context

- 用途：修复 11 处异步 usage-record 闭包在 worker 执行时才调用 `clientRequestedUsageFields(c, ...)` 的 bug。gin 在 handler 返回后把 `*gin.Context` 放回 sync.Pool 复用，延迟读 c 会拿到其他并发请求的 RequestedPublicModel（计费归因串号）+ data race，且闭包钉住整个 context 引用图（含完整请求体）直到队列(上限 16384)排空。机制由证据测试确定性复现（gin 池复用后延迟求值读到 B 请求的模型）。
- 涉及：
  - 修改（每处一行 hoist + 一行替换，与上游 `:535` 自己 hoist ForceCacheBilling 的既有模式一致）：`gateway_handler.go`(×2)、`gateway_handler_chat_completions.go`、`gateway_handler_responses.go`、`gemini_v1beta_handler.go`、`openai_chat_completions.go`、`openai_embeddings.go`、`openai_gateway_handler.go`(×3)、`openai_images.go`。
  - 新增：`backend/internal/handler/gateway_usage_context_capture_evidence_test.go`。
- 退役条件：上游自行修复所有调用点（rebase 时若上游已把求值挪到闭包外，丢弃本 patch 对应 hunk；证据测试的 deferred 子测试若因 gin 池语义变化而失败，整个 patch 连同测试一起退役）。

## perf-prompt-cache-key-inject — CC API-key 分支注入去 map 往返

- 用途：`forwardChatCompletions` API-key 分支为注入一个 `prompt_cache_key` 字段把整个 responsesBody unmarshal 成 `map[string]any` 再 marshal 回来，实测 churn 6.21×；提取为 `injectPromptCacheKeyIfAbsent` 并改用 gjson/sjson 后 1.02×。行为语义由 5 个等价用例锁定（含 blank/非字符串/无效 JSON 边界）。注意：OAuth 分支的 map 是 `applyCodexOAuthTransformWithOptions` 的承重结构，刻意不动。
- 涉及：
  - 新增：`backend/internal/service/openai_prompt_cache_key.go`、`openai_prompt_cache_key_test.go`。
  - 修改：`openai_gateway_chat_completions.go` API-key 分支 15 行换 6 行调用。
- 退役条件：上游重构该注入路径（如自己改用 sjson 或将 prompt_cache_key 并入转换器），rebase 冲突时以上游为准并删除本 patch。
