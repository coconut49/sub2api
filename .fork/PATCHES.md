# Fork Patch 清单

`main` 上位于上游 base tag 之后的每个 commit 在此登记一个条目。新增、修改、删除 patch 时同步更新本文件。

## fork-infra — fork 基础设施

- 用途：fork 的自我描述与自动化。CLAUDE.md、`.fork/`、`fork-sync.yml`、`fork-image.yml`。
- 退役条件：无，fork 存续期间永久保留。

## opus-5 — 提前采纳上游 claude-opus-5 适配

- 用途：网关支持 `claude-opus-5`。上游已在 release 后的 commit `6c9b84cc7`（feat: 适配 Anthropic 新模型 claude-opus-5）实现，本 patch 是该 commit 的原样 cherry-pick，diff 与上游逐字节一致。除模型登记外，它还修复定价兜底 3 倍超收与 Bedrock 版本闸门降级两个静默 bug。
- 涉及：以上游 commit 为准（backend constants / bedrock_request / billing_service / pricing_service / pricing JSON / 前端白名单与 scope 简称，含回归测试 `claude_opus5_test.go`）。pricing JSON 的改动是上游自己的内容，不违反「不本地修改该文件」规则。
- 退役条件：rebase 到包含 `6c9b84cc7` 的上游 release tag 时，git 因 patch-id 相同自动丢弃本 commit；届时删除本条目即可。
