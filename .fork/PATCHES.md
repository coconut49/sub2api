# Fork Patch 清单

`main` 上位于上游 base tag 之后的每个 commit 在此登记一个条目。新增、修改、删除 patch 时同步更新本文件。

登记格式约定：性能/正确性类 patch 用票据体。标题 = `type(scope): 缺陷/变更的精确陈述 + 可量化影响`（与 Jira/Linear issue title 同标准：可讲清、可委派、可 review，不用比喻）；正文 = 当前行为 / 预期行为 / 如何修复 / 影响面与风险 / 涉及文件 / 退役条件。读者假定为不熟悉本仓库与业务背景的专业软件工程师：技术名词（worker pool、sync.Pool、gjson 等）保留原文，需要解释的是仓库特有的业务链路。

## fork-infra — fork 基础设施

- 用途：fork 的自我描述与自动化。CLAUDE.md、`.fork/`、`fork-sync.yml`、`fork-image.yml`。
- 生命周期：长期维护，不适用退役——这是 fork 自身的基础设施，上游没有对应物，不随上游演进而消亡。

## opus-5 — 提前采纳上游 claude-opus-5 适配

- 用途：网关支持 `claude-opus-5`。上游已在 release 后的 commit `6c9b84cc7`（feat: 适配 Anthropic 新模型 claude-opus-5）实现，本 patch 是该 commit 的原样 cherry-pick，diff 与上游逐字节一致。除模型登记外，它还修复定价兜底 3 倍超收与 Bedrock 版本闸门降级两个静默 bug。
- 涉及：以上游 commit 为准（backend constants / bedrock_request / billing_service / pricing_service / pricing JSON / 前端白名单与 scope 简称，含回归测试 `claude_opus5_test.go`）。pricing JSON 的改动是上游自己的内容，不违反「不本地修改该文件」规则。
- 退役条件：rebase 到包含 `6c9b84cc7` 的上游 release tag 时，git 因 patch-id 相同自动丢弃本 commit；届时删除本条目即可。

## perf-evidence — Ticket 0 · test(gateway): 建立请求热路径内存分配测量基线,量化各环节放大倍数

- **背景**：sub2api 是把 Claude/OpenAI 订阅账号转成 API 服务的网关。上游 issue #4365（v0.1.156 内存暴涨、Codex 长上下文场景下几个并发即 OOM）、#1465（上游账号异常时内存占用高）指向请求处理链存在内存放大，但均无量化数据。
- **当前行为**：无法回答"一个请求的内存开销是请求体的几倍、热点在哪一步"。
- **预期行为**：每个可疑热点有一个可复现的 allocation 测量测试（`runtime.MemStats` 计 churn/live），给出实测倍数；测试同时内置"问题存在"断言作为退役哨兵——上游修复后断言变红，提示删除对应 fork patch。
- **如何修复**：只加测量测试，不改生产代码。实测结论（Go 1.26，churn = 累计分配，live = 同时存活）：
  - 请求体读取（`httputil.ReadRequestBodyWithPrealloc`）：带 Content-Length 仅 1.13×，无问题（静态分析预判 ~2×，被测试证伪）；无 Content-Length（chunked/h2c）4.0×。
  - Chat Completions → Responses 转换链（Codex 路径）：churn 15.7×、live 7.2×。
  - prompt 审计快照（`securityaudit`）：churn 25.3×、live 1.2×——GC 压力型；本 fork 未启用该功能，零优先级。
  - `/v1/messages` 改写链逐步测量：Claude Code 客户端路径总 churn 8.5×，其中 FilterThinkingBlocks 单步 6.4×（→ Ticket 3 已修复归零）；OAuth 账号 + 非 Claude Code 客户端的 mimicry 路径总 churn 49.3×，applyToolsLastCacheBreakpoint 单步 28.3×（不修：本 fork 流量走不到该路径）。
- **影响面与风险**：全部新增测试文件，零上游冲突面，不影响运行时。
- 涉及（全部新增）：
  - `backend/internal/pkg/httputil/body_amplification_evidence_test.go`
  - `backend/internal/pkg/apicompat/chatcompletions_amplification_evidence_test.go`
  - `backend/internal/securityaudit/prompt_amplification_evidence_test.go`
  - `backend/internal/service/gateway_forward_chain_amplification_evidence_test.go`
- **退役条件**：某测试的"amplification appears fixed"断言失败 = 上游已优化对应路径，删除该测试并退役依赖它的优化 patch。

## fix-usage-ctx-capture — Ticket 1 · fix(handler): 异步 usage 闭包在 handler 返回后读取 gin.Context,context 池复用导致跨请求计费归因错误与 data race(11 处调用点)

- **当前行为**：请求结束后，usage 记录经有界 worker pool 异步落库。提交给 worker 的闭包捕获了 `*gin.Context`，并在**闭包执行时**才调用 `clientRequestedUsageFields(c, ...)` 从 `c.Request.Context()` 读取客户端请求的模型名。但 handler 返回后 gin 会把 Context 放回 sync.Pool 并绑定到下一个请求，因此 worker 繁忙时闭包读到的是**另一个并发请求**的 RequestedPublicModel——usage 记录的渠道归因字段串号，同时构成 data race；闭包还把整个 Context 引用图（含完整请求体）钉在队列中（上限 16384 条）。
- **预期行为**：所有传给异步闭包的数据在 handler 栈上求值完毕，闭包只捕获标量/值，不持有 `*gin.Context`。
- **如何修复**：全仓库普查出 11 处同模式调用点（最初分析只发现 3 处），全部改为提交闭包前求值（每处 diff 两行），写法与上游自己 hoist `ForceCacheBilling` 的既有模式一致。新增测试确定性复现串号机制：gin 池复用后延迟求值读到第二个请求的模型名。
- **影响面与风险**：仅影响 usage 日志的 ChannelUsageFields 四个归因字段，不碰转发逻辑与响应。求值输入（channelMapping、reqModel、UpstreamModel、context 内的 RequestedPublicModel）在 handler 生命周期内均不变，延迟求值不存在能读到"更新状态"的合法场景——旧行为的正确输出是新行为的严格子集，无 bug-as-feature 风险。
- 涉及：
  - 修改：`gateway_handler.go`(×2)、`gateway_handler_chat_completions.go`、`gateway_handler_responses.go`、`gemini_v1beta_handler.go`、`openai_chat_completions.go`、`openai_embeddings.go`、`openai_gateway_handler.go`(×3)、`openai_images.go`。
  - 新增：`backend/internal/handler/gateway_usage_context_capture_evidence_test.go`。
- **退役条件**：上游自行修复所有调用点（rebase 冲突时以上游为准，丢弃对应 hunk）；证据测试的 deferred 子测试若因 gin 池语义变化而失败，patch 连同测试一起退役。

## perf-prompt-cache-key-inject — Ticket 2 · perf(service): chat_completions API-key 路径注入 prompt_cache_key 时对整个请求体做 map[string]any 往返,分配放大 6.2x→1.0x

- **当前行为**：`/v1/chat/completions` 请求经 API-key 类型的 OpenAI 账号转发时，需在已序列化的 Responses 请求体上注入 `prompt_cache_key` 字段（会话级 prompt 缓存路由键）。原实现将整个 body `json.Unmarshal` 成 `map[string]any`、set 一个键、再 `json.Marshal` 回来——实测 churn 为 body 的 6.21×。
- **预期行为**：写入单个顶层字段只付约一次 body 复制的代价（~1×）。
- **如何修复**：抽取 `injectPromptCacheKeyIfAbsent`，改用 gjson 判存 + `sjson.SetBytes` 原地写入。实测 6.21× → 1.02×。TDD：5 个行为等价用例（已有非空值保留、blank/非字符串覆盖、无效 JSON 报错）先锁语义，2× alloc 天花板断言驱动重写并防回归。
- **影响面与风险**：仅"OpenAI 平台 + API-key 账号 + chat/completions 入口 + 配置了缓存键"路径。OAuth（Codex 订阅）分支刻意不动——那里的 `map[string]any` 是 `applyCodexOAuthTransformWithOptions` 的工作数据结构，属承重墙。唯一字节级差异：JSON 键不再被 map 往返重排序（JSON 对象语义无序，上游不感知）。
- 涉及：
  - 新增：`backend/internal/service/openai_prompt_cache_key.go`、`openai_prompt_cache_key_test.go`。
  - 修改：`openai_gateway_chat_completions.go` API-key 分支 15 行换 6 行调用。
- **退役条件**：上游重构该注入路径（改用 sjson 或将注入并入转换器），rebase 冲突时以上游为准并删除本 patch。

## perf-thinking-precheck — Ticket 3 · perf(service): FilterThinkingBlocks 对无需过滤的 thinking 请求仍执行全量泛型反序列化,增加 gjson 预检后该步 6.4x→0,整链 8.5x→2.1x

- **背景**：Anthropic extended thinking 下，assistant 历史消息含带 `signature` 的 thinking block，客户端多轮对话时原样回传；签名缺失/无效会被上游 400 拒绝，故网关在转发前调用 `FilterThinkingBlocks` 剔除坏块。
- **当前行为**：该函数已有字节级 fast path（`bytes.Contains` 探测 thinking 关键字），但开启 thinking 的请求顶层必带 `"thinking"` 字段，探测必然命中穿透，于是**签名齐全、无需剔除任何块的常态请求**（Claude Code 主流量）也要把整个 body `json.Unmarshal` 成 `map[string]any` 检查一遍：单步 churn 6.4×，占整条改写链 8.5× 的大头。同链路的 StripEmptyTextBlocks / FilterWebSearchHistoryBlocks 均有有效 fast path（实测 0×），唯独此步缺失。
- **预期行为**：先低成本预检"是否存在会被剔除的块"，无则原样返回；仅真需剔除时才走全量解析。剔除语义完全不变。
- **如何修复**：新增 `thinkingBlocksNeedFiltering`——gjson 零拷贝惰性遍历（unsafe 字符串别名 + `gjson.Get`，与上游 `FilterThinkingBlocksForRetry` 同款模式；注意 `gjson.GetBytes`/`ParseBytes` 内部有整树安全拷贝，不可用），判定条件与完整路径的剔除条件逐条对应。在 `filterThinkingBlocksInternal` 字节 fast path 之后插入 3 行短路。实测：常态请求该步 6.4× → **0.00×**（零分配），Claude Code 客户端整链 8.5× → **2.09×**。
- **影响面与风险**：所有 anthropic-strict 模型的 `/v1/messages` 请求（本 fork 主流量）。9 个行为用例锁定语义（签名缺失/dummy 签名/thinking 未开启/非 assistant 角色/typeless 块/字符串 content/非 strict 模型等边界）。预检判"需要剔除"时走原逻辑；判错的最坏后果是退回旧行为（多花内存，结果仍正确）。已知偏差：JSON 重复键时 gjson 取首个、`encoding/json` 取末个——body 入口已过校验且正规客户端不产生重复键，即使出现也只是多走一次全量路径或保持原样。
- 涉及：
  - 新增：`backend/internal/service/gateway_request_thinking_precheck.go`、`filter_thinking_blocks_fork_test.go`。
  - 修改：`gateway_request.go` 的 `filterThinkingBlocksInternal` 插入 3 行短路（含注释共 7 行）。
- **退役条件**：上游为 FilterThinkingBlocks 自行加上等效 fast path（rebase 后 `filter_thinking_blocks_fork_test.go` 的 AllocCeiling 测试仍绿），删除本 fork 的预检实现，保留测试作回归守卫。
