# 项目开发规范

本规范适用于人工和 AI 辅助开发。Go 模块位于仓库根目录，管理台源码位于 `web/`，Go 服务嵌入的管理台产物位于 `internal/admin/dist/index.html`。

## 改动约定

- 从实际入口追踪行为。HTTP 路由在 `internal/server`，网关逻辑在 `internal/gateway`，用量记录与持久化在 `internal/usagejournal`。复用已有类型和错误处理，保持改动聚焦。
- Go 文件用 `gofmt` 格式化。错误在需要上下文时用 `fmt.Errorf` 配合 `%w` 包装。测试使用 `t.TempDir()` 和受控输入，外部服务用测试替身。
- 修改 API 或持久化时，检查已有调用方和旧版数据。涉及时间范围时，明确时区、起止边界和保留期，并确认图表、汇总、筛选、分页使用预期的数据范围。
- 用量的每日汇总按本地日历日保留，从本地日期往前 90 天的午夜起算，包含起始日和今天。请求明细默认最多 400 条，按账号或模型筛选时以仍保留的明细为基础。用例应覆盖旧快照重开、临界日期和明细容量。
- 管理台行为优先改 `web/src/`。修改管理台后使用项目的构建命令重新生成嵌入产物，确保 Go 二进制交付的是同一版本。

## 本地验证

新克隆可以运行 `git config core.hooksPath .githooks` 启用仓库 Hook。pre-commit 检查暂存 Go 文件的格式，并对工作区中的受影响 Go 包运行测试。提交前确认暂存内容和测试过的工作区一致，尤其是部分暂存时。

从根目录运行以下检查。以用量相关改动为例，先跑相关包，再跑全量检查。CI 另外执行 Go race 检测。

```sh
go test -count=1 ./internal/usagejournal ./internal/gateway
go vet ./...
go test -count=1 ./...
git diff --check
```

管理台改动从 `web/` 执行以下命令。Windows PowerShell 如果拦截 pnpm 脚本，可调用 `pnpm.cmd`。

```sh
pnpm install --frozen-lockfile
pnpm run typecheck
pnpm test
pnpm run build
```

## AI 辅助开发

1. 在任务里写清当前现象、期望行为、兼容范围和验收样例。历史查询可以直接给出旧版快照中应保留和应排除的日期。
2. 先定位入口和已有测试。修复回归时先写出会因目标问题而失败的测试，再做最小改动，复跑同一个用例。
3. 在审查中优先看用户路径、跨模块契约、旧数据、空值和边界。用格式化、静态分析和 Hook 承担可机械判定的检查。
4. 交付时报告实际执行的命令和结果。测试失败时尽量在改动前的版本对照运行，记录仍失败的用例和影响。
