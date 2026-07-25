# coconut49/sub2api — Wei-Shaw/sub2api 的长期维护 fork

`main` = 最新上游 release tag + 一叠 fork patch，通过 rebase 跟进上游。所有维护知识都在仓库内显式记录，不依赖任何本地记忆。

- 维护手册（分支模型、同步流程、发布约定、环境事实）：@.fork/MAINTENANCE.md
- Patch 清单（每个定制的用途、涉及文件、退役条件）：@.fork/PATCHES.md

## 硬规则

- 查看当前 fork delta：`git log $(git merge-base main upstream/main)..main`。改动 patch 栈时同步更新 `.fork/PATCHES.md`。
- 每个定制做成一个语义完整的 commit，以最小冲突面实现：优先新增文件、新增表项、注册点插一行；避免重写上游代码。
- 不修改 `backend/resources/model-pricing/model_prices_and_context_window.json`：上游会整体再生成该文件，任何本地改动必然冲突；运行时定价走远程 LiteLLM 数据，新模型无需改此文件。
- `main` 允许 force-push；`v*-fork.*` tag 一旦推出即不可变。
- 上游操作走 `upstream` remote（https://github.com/Wei-Shaw/sub2api.git）；没有该 remote 时先 `git remote add upstream` 并 `git fetch upstream --tags`。
- 不向上游提 PR，也不为上游兼容性做额外工作。
