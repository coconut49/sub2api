你在一个 GitHub Actions runner 里，当前仓库正处于一次被冲突中断的 `git rebase` 中：fork patch 栈正在被重放到新的上游 release tag 上。

任务：解决所有冲突并完成整个 rebase，使 `main` 重新等于「新上游 tag + 完整 patch 栈」。

步骤：

1. 读 `git show origin/main:.fork/PATCHES.md` 与 `git show origin/main:.fork/MAINTENANCE.md`（工作树处于 rebase 中途，这两个文件以 origin/main 版本为准），理解每个 patch 的用途与冲突处理原则。
2. `git status` 与 `git diff` 查看冲突；用 `git log --oneline REBASE_HEAD -1` 确认当前正在重放哪个 patch。
3. 逐个解决冲突：保留上游新实现 + 保持该 patch 的语义意图。若上游已实现该 patch 的等价功能，采用上游实现，用 `git rebase --skip` 跳过该 patch，并在 rebase 完成后更新 `.fork/PATCHES.md` 删除对应条目（作为一个新的整理 commit 并入相邻 patch 或单独 commit 进栈尾）。
4. `git add` 后 `git rebase --continue`，重复直到 rebase 完成。
5. 完成后自查：`git log $(git merge-base HEAD upstream/main)..HEAD --oneline` 应与 `.fork/PATCHES.md` 条目一一对应；编译最容易受影响的包（如 `go build ./...`）确认无语法级错误。

禁止：`git push`（由工作流后续步骤负责）、`git rebase --abort`、修改与冲突无关的代码。
