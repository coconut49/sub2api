# Fork Patch 清单

`main` 上位于上游 base tag 之后的每个 commit 在此登记一个条目。新增、修改、删除 patch 时同步更新本文件。

性能/正确性类 patch 用票据体登记（当前行为 / 预期行为 / 如何修复 / 影响面与风险 / 涉及文件 / 退役条件），先讲人话再讲文件，方便日后回顾"当初修了什么、为什么修"。

## fork-infra — fork 基础设施

- 用途：fork 的自我描述与自动化。CLAUDE.md、`.fork/`、`fork-sync.yml`、`fork-image.yml`。
- 退役条件：无，fork 存续期间永久保留。

## opus-5 — 提前采纳上游 claude-opus-5 适配

- 用途：网关支持 `claude-opus-5`。上游已在 release 后的 commit `6c9b84cc7`（feat: 适配 Anthropic 新模型 claude-opus-5）实现，本 patch 是该 commit 的原样 cherry-pick，diff 与上游逐字节一致。除模型登记外，它还修复定价兜底 3 倍超收与 Bedrock 版本闸门降级两个静默 bug。
- 涉及：以上游 commit 为准（backend constants / bedrock_request / billing_service / pricing_service / pricing JSON / 前端白名单与 scope 简称，含回归测试 `claude_opus5_test.go`）。pricing JSON 的改动是上游自己的内容，不违反「不本地修改该文件」规则。
- 退役条件：rebase 到包含 `6c9b84cc7` 的上游 release tag 时，git 因 patch-id 相同自动丢弃本 commit；届时删除本条目即可。

## perf-evidence — 请求路径内存放大证据测试（Ticket 0：测量基线）

- **当前行为**：上游 issue #4365（v0.1.156 内存暴涨，Codex 长上下文场景）、#1465（上游账号异常时内存占用高）反映网关处理请求存在内存放大，但没有量化数据说明放大发生在哪一步、多少倍。
- **预期行为**：每个可疑热点都有一个可复现的测量测试给出实测倍数，作为后续优化的 TDD 基线；上游哪天修好了，对应测试的"问题存在"断言会变红，提示退役。
- **如何修复**：不修代码，只加测量测试。实测结论（Go 1.26）：
  - 请求体读取：带 Content-Length 仅 1.13×（无问题，伪靶点）；无 Content-Length（chunked/h2）churn 4.0×。
  - CC→Responses 转换链：churn 15.7×、同时存活 7.2×。
  - prompt 审计快照：churn 25.3×、存活仅 1.2×（GC 压力型；本 fork 未开启该功能，零优先级）。
  - /v1/messages 改写链逐步测量：Claude Code 客户端路径总 churn 8.5×，其中 FilterThinkingBlocks 单步 6.4×（→ 已由 Ticket 3 修复归零）；第三方客户端 mimicry 路径总 churn 49.3×，applyToolsLastCacheBreakpoint 单步 28.3×（未修，本 fork 流量走不到）。
- **影响面与风险**：全部新增测试文件，零上游冲突面，不影响运行时。
- 涉及（全部新增）：
  - `backend/internal/pkg/httputil/body_amplification_evidence_test.go`
  - `backend/internal/pkg/apicompat/chatcompletions_amplification_evidence_test.go`
  - `backend/internal/securityaudit/prompt_amplification_evidence_test.go`
  - `backend/internal/service/gateway_forward_chain_amplification_evidence_test.go`
- **退役条件**：某个测试的"amplification appears fixed"断言开始失败，即说明上游已优化对应路径，删除该测试文件并退役依赖它的优化 patch。

## fix-usage-ctx-capture — Ticket 1：计费记录可能张冠李戴

- **当前行为**：每笔请求结束后，网关把"记账"动作交给后台工人异步执行，但账目里"客户端请求的模型名"这一项是工人**执行时才回头去翻请求上下文**查的。请求一结束，gin 就把上下文对象回收给下一个请求复用，所以后台繁忙时工人可能翻到**别人的上下文**——A 用户的账单写上 B 用户的模型名（计费归因串号），同时构成数据竞争，且闭包把整个上下文引用图（含完整请求体）钉在队列里（上限 16384 条）。
- **预期行为**：账目内容在请求还活着时就抄录成值，交给后台工人的是写好的纸条；谁的请求记谁的模型，永远不串。
- **如何修复**：11 处调用点全部改为"提交闭包前求值、闭包只捕获结果值"，照抄上游自己 hoist ForceCacheBilling 的既有写法（每处 diff 两行）。证据测试确定性复现了串号机制（gin 池复用后延迟求值读到 B 请求的模型）。
- **影响面与风险**：仅影响 usage 日志的渠道归因字段，不碰转发逻辑与响应。延迟求值不可能拿到任何"更晚的合法状态"（输入在 handler 期间全部不变），旧行为的正确输出是新行为的严格子集，无"bug 已成 feature"空间。
- 涉及：
  - 修改：`gateway_handler.go`(×2)、`gateway_handler_chat_completions.go`、`gateway_handler_responses.go`、`gemini_v1beta_handler.go`、`openai_chat_completions.go`、`openai_embeddings.go`、`openai_gateway_handler.go`(×3)、`openai_images.go`。
  - 新增：`backend/internal/handler/gateway_usage_context_capture_evidence_test.go`。
- **退役条件**：上游自行修复所有调用点（rebase 时若上游已把求值挪到闭包外，丢弃本 patch 对应 hunk）；证据测试的 deferred 子测试若因 gin 池语义变化而失败，整个 patch 连同测试一起退役。

## perf-prompt-cache-key-inject — Ticket 2：为补一行字誊写整封信

- **当前行为**：Codex 流量走 API-key 账号时，转发前要在请求上补一个缓存标签（`prompt_cache_key` 字段）。实现方式是把整个请求体（可能几百 KB）完整拆解成内存对象、补一个字段、再完整重新组装，实测内存开销为请求体的 6.21×。
- **预期行为**：补一个字段只付"补一个字段"的代价（约一次复制，~1×）。
- **如何修复**：提取 `injectPromptCacheKeyIfAbsent` 函数并改用 gjson/sjson 原文编辑，实测 6.21× → 1.02×。行为边界由 5 个等价用例锁定（已有非空值保留原样、blank/非字符串覆盖、无效 JSON 报错），2× alloc 天花板断言防回归。
- **影响面与风险**：仅"OpenAI 平台 + API-key 账号 + chat/completions 入口 + 配置了缓存键"这一条窄路径。OAuth（Codex 订阅）分支的 map 是 codex transform 的承重结构，刻意不动。唯一字节级差异是 JSON 键不再被重排序（JSON 语义无序，上游不感知）。
- 涉及：
  - 新增：`backend/internal/service/openai_prompt_cache_key.go`、`openai_prompt_cache_key_test.go`。
  - 修改：`openai_gateway_chat_completions.go` API-key 分支 15 行换 6 行调用。
- **退役条件**：上游重构该注入路径（如自己改用 sjson 或将 prompt_cache_key 并入转换器），rebase 冲突时以上游为准并删除本 patch。

## perf-thinking-precheck — Ticket 3：每个请求都做全身安检，哪怕检不出任何东西

- **当前行为**：多轮对话时客户端会把历史消息（含模型以前的 thinking 推理块）原样发回，个别块可能缺防伪签名导致上游拒单，所以转发前要检查并剔除坏块——检查本身必要，但方式是把整个请求体拆解成泛型内存对象来翻查。开启 extended thinking 的请求必带顶层 `"thinking"` 字段，原有的字节级 fast path 必然穿透，于是**签名齐全、无需剔除任何块的常态请求**（Claude Code 主流量）也要付全量解析的代价：单步 churn 6.4×，占整条改写链 8.5× 的大头。
- **预期行为**：像安检先过金属探测门——先零成本扫一眼"有没有坏块"，没有就原样放行；只有真的存在坏块时才付全套拆解的代价。剔除语义完全不变。
- **如何修复**：新增 `thinkingBlocksNeedFiltering` 预检（gjson 零拷贝惰性遍历，判定条件与完整路径逐条对应），在 `filterThinkingBlocksInternal` 的字节 fast path 之后插入 3 行短路。实测：常态请求该步 6.4× → **0.00×**（字面零分配），Claude Code 客户端整链 8.5× → **2.09×**。
- **影响面与风险**：所有 anthropic-strict 模型的 `/v1/messages` 请求（本 fork 主流量）。风险低：预检对每种剔除条件逐条镜像完整路径的判定，9 个行为用例锁定语义（含签名缺失/dummy/thinking 关闭/非 assistant/typeless 块等边界）；预检结论为"需要剔除"时走原逻辑，结论错误的最坏后果是退回旧行为（多花内存但结果正确）。已知偏差：JSON 重复键时 gjson 取首个、encoding/json 取末个——body 入口已校验且正规客户端不产生重复键，即使出现也只是退回完整路径或保持原样。
- 涉及：
  - 新增：`backend/internal/service/gateway_request_thinking_precheck.go`、`filter_thinking_blocks_fork_test.go`。
  - 修改：`gateway_request.go` 的 `filterThinkingBlocksInternal` 插入 3 行短路（含注释共 7 行）。
- **退役条件**：上游给 FilterThinkingBlocks 自行加上等效 fast path（`filter_thinking_blocks_fork_test.go` 的 AllocCeiling 测试在 rebase 后仍绿即可直接删除本 fork 的预检实现，保留测试作回归守卫）。
