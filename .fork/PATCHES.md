# Fork Patch 清单

`main` 上位于上游 base tag 之后的每个 commit 在此登记一个条目。新增、修改、删除 patch 时同步更新本文件。

## fork-infra — fork 基础设施

- 用途：fork 的自我描述与自动化。CLAUDE.md、`.fork/`、`fork-sync.yml`、`fork-image.yml`。
- 退役条件：无，fork 存续期间永久保留。

## opus-5 — 适配 claude-opus-5

- 用途：网关支持 Anthropic 最新旗舰 `claude-opus-5`（2026-07-24 发布，$5/$25 每 MTok，原生 1M 上下文，128k 输出，adaptive thinking）。
- 涉及：
  - `backend/internal/domain/constants.go`：DefaultBedrockModelMapping 增加 `claude-opus-5 → us.anthropic.claude-opus-5-v1`。
  - `backend/internal/pkg/claude/constants.go`：DefaultModels 增加条目（Claude Code 客户端模型列表）。
  - `backend/internal/service/settings_view.go`：`context-1m-2025-08-07` beta 默认白名单放行 opus-5 各路径变形（直连 / Vertex `@` / Bedrock 区域前缀）。
  - `backend/internal/service/gateway_beta_test.go`：追加 opus-5 表驱动测试。
  - `frontend/src/composables/useModelWhitelist.ts`：claudeModels 白名单 + Anthropic/Bedrock 预设映射按钮。
- 实现模板：镜像上游 sonnet-5 的引入方式（上游 commit `db0414233`，`feat: 适配 sonnet5`）；有意不动 Antigravity 相关映射（Antigravity 上游无 5 系模型，与 sonnet-5 处理一致）；不动 pricing JSON（运行时定价走远程 LiteLLM 数据）。
- 退役条件：上游发布 claude-opus-5 支持后删除本 patch，改用上游实现；若上游实现缺 1M beta 白名单，仅保留 settings_view 部分并更新本条目。
