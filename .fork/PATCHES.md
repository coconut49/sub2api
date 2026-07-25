# Fork Patch 清单

`main` 上位于上游 base tag 之后的每个 commit 在此登记一个条目。新增、修改、删除 patch 时同步更新本文件。

登记格式约定：本文件是 rebase 时的作战地图,只保留三样东西——patch ID + 一行标题、issue 链接、退役条件。问题描述、acceptance criteria、影响面分析都在对应 issue 里;改了哪些代码以 commit 为准(commit message 带 `Refs: #N` 关联 issue,引用 commit 一律用标题不用 SHA——main 会 rebase,SHA 不稳定)。

## fork-infra — fork 基础设施

- 用途：fork 的自我描述与自动化。CLAUDE.md、`.fork/`、`fork-sync.yml`、`fork-image.yml`。
- 生命周期：长期维护，不适用退役——这是 fork 自身的基础设施，上游没有对应物，不随上游演进而消亡。

## opus-5 — 提前采纳上游 claude-opus-5 适配

- 用途：网关支持 `claude-opus-5`。上游已在 release 后的 commit `6c9b84cc7`（feat: 适配 Anthropic 新模型 claude-opus-5）实现，本 patch 是该 commit 的原样 cherry-pick，diff 与上游逐字节一致。除模型登记外，它还修复定价兜底 3 倍超收与 Bedrock 版本闸门降级两个静默 bug。
- 涉及：以上游 commit 为准（backend constants / bedrock_request / billing_service / pricing_service / pricing JSON / 前端白名单与 scope 简称，含回归测试 `claude_opus5_test.go`）。pricing JSON 的改动是上游自己的内容，不违反「不本地修改该文件」规则。
- 退役条件：rebase 到包含 `6c9b84cc7` 的上游 release tag 时，git 因 patch-id 相同自动丢弃本 commit；届时删除本条目即可。

## perf-evidence — test(gateway): 请求热路径内存分配测量基线

- Issue：https://github.com/coconut49/sub2api/issues/1
- 退役条件：某测试的 "amplification appears fixed" 断言失败 = 上游已优化对应路径，删除该测试并退役依赖它的优化 patch。

## fix-usage-ctx-capture — fix(handler): 异步 usage 闭包延迟读已复用的 gin.Context,跨请求计费归因错误与 data race

- Issue：https://github.com/coconut49/sub2api/issues/2
- 退役条件：上游自行修复所有调用点（rebase 冲突时以上游为准，丢弃对应 hunk）；证据测试的 deferred 子测试若因 gin 池语义变化而失败，patch 连同测试一起退役。
