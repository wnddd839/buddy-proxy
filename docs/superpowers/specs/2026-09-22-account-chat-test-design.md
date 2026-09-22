# Design: 账号 Chat 测试（单账号 + 批量）

**日期:** 2026-09-22  
**状态:** Approved（方案 1）

## 目标

管理台账号池对齐 New API「渠道测试」：对钉死账号发一条最小 chat，验证可用性与延迟；支持单账号与批量。

## 非目标

- 不走 `CompleteFromPool`（禁止失败换号）
- 不写回 cooldown / 选号 / session pin / MarkResult
- 不改 production chat 路径行为

## API

| Method | Path | Body |
|--------|------|------|
| POST | `/direct-admin/api/codebuddy/accounts/{id}/test` | 可选 `{"model":"..."}` |
| POST | `/direct-admin/api/codebuddy/test` | 可选 `{"site":"...","model":"..."}` |

- 批量路由与 `checkin` 同级，避免被 `accounts/{id}` 前缀吞掉
- `model` 空：从 `ListModels(fresh=false)` 取 `creditMultiplier` 最低者；并列取列表顺序第一；无模型则错误
- 单测：任意有凭证账号（含 disabled，便于排查）
- 批量：仅当前号池 **enabled + 有凭证** 账号；默认 `site=ActivePoolSite()`

## 行为

- 载荷：`user: "ping"`，`max_completion_tokens=8`；上游仍走 protocol_direct 流式聚合（与 `Provider.Complete` 一致），管理台拿 JSON
- 批量：串行，账号间隔 350ms（同 IP ~1s/3 次风控）
- 超时：单账号 20s；批次 `min(20s×N, 5m)`，耗尽后未测账号记 `skipped`
- 响应对齐 checkin：`ok` / per-account results / summary（`omitzero`）

## 实现落点

- `internal/models`：`LowestMultiplierID`
- `internal/gateway/chattest.go`：`TestAccountChat` / `RunPoolChatTest`（复用 `chatOptionsFromAccount`）
- `internal/server/server.go`：路由接线
- `internal/admin/page.go`：行内「测试」+ 顶部「批量测试」+ 共享模型下拉
- `docs/api/http.md`：文档同步

## 测试

- 单元：最低倍率选取、JSON omitzero、钉死账号不换号、批量串行汇总/超时跳过
- 路由：单测 / 批量路径与未知 action 404
