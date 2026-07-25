# Fork Patch 清单

`main` 上位于上游 base tag 之后的每个 commit 在此登记一个条目。新增、修改、删除 patch 时同步更新本文件。

登记格式约定：本文件是 rebase 时的作战地图,只保留三样东西——patch ID + 一行标题、issue 链接、退役条件。问题描述、acceptance criteria、影响面分析都在对应 issue 里;改了哪些代码以 commit 为准(commit message 带 `Refs: #N` 关联 issue,引用 commit 一律用标题不用 SHA——main 会 rebase,SHA 不稳定)。

## fork-infra — fork 基础设施

- 用途：fork 的自我描述与自动化。CLAUDE.md、`.fork/`、`fork-sync.yml`、`fork-image.yml`。
- 生命周期：长期维护，不适用退役——这是 fork 自身的基础设施，上游没有对应物，不随上游演进而消亡。

## perf-evidence — test(gateway): 请求热路径内存分配测量基线

- Issue：https://github.com/coconut49/sub2api/issues/1
- 退役条件：某测试的 "amplification appears fixed" 断言失败 = 上游已优化对应路径，删除该测试并退役依赖它的优化 patch。
