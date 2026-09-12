# Architecture Overview

## 目标

把 CodeBuddy 上游 `protocol_direct` 能力封装为 OpenAI 兼容网关：

```text
Client (OpenAI SDK / curl / NewAPI / ZCode)
   │  /v1/chat/completions  (SSE or JSON)
   ▼
server ──► gateway ──► accounts pool + oauth refresh
                 │
                 ▼
              provider (protocol_direct)
                 │
                 ▼
         CodeBuddy upstream
         POST /v2/chat/completions
         GET  /v3/config
```

## 包职责

| 包 | 职责 | 不应包含 |
|----|------|----------|
| `config` | 环境变量 / `.env` 解析，默认值与归一化 | 业务逻辑 |
| `accounts` | 账号池（内存权威 + 异步刷盘）、按额度选号、统计 | HTTP |
| `sessionpin` | 同会话钉在一个账号（进程内 TTL 表） | 号池轮询 |
| `usagejournal` | 用量明细环形缓冲、按日汇总、可选 `proxy-usage.json` 落盘 | HTTP / 管理台 HTML |
| `version` | 构建时 `-ldflags` 注入的发布号（`Makefile VERSION=`） | 业务逻辑 |
| `oauth` | OAuth 发起 / 轮询 / refresh / JWT 解析 | 路由 |
| `provider` | 上游请求头、SSE 解析、事件累积、Usage 归一化 | 账号选择策略 |
| `models` | 模型发现与 public id 规范化 | 流式聊天 |
| `gateway` | 选号、额度快照探活、失败换号、会话粘滞调度、stats、用量 journal 记录、OAuth session、运行时配置（`atomic.Pointer`） | HTML |
| `server` | 路由、鉴权、流式写回 | 上游协议细节 |
| `admin` | 管理台页面字符串 | 业务状态机 |
| `billing` | Credits 查询（剩余 / 总额）与通知码解析 | 账号写入 |
| `openai` | OpenAI chat/chunk/usage 结构与上游错误分类 | 上游映射 |
| `strutil` | `First` / `Truncate` / `MaskSecret` / `RandomHex` / `Compact` | — |
| `httputil` | JSON/SSE/Cookie/Origin/CSRF | — |

## 设计原则

1. **单传输**：只维护 `protocol_direct`，不恢复 Node 时代多传输路径
2. **零第三方运行时依赖**（标准库优先，`go.mod` 无 require）
3. **上下文可取消**：上游请求绑定 `context.Context`，客户端断开即取消
4. **账号与用量写盘**：内存为权威 + 250ms 合并异步刷盘；账号凭据变更同步刷盘；`proxy-accounts.json` / `proxy-usage.json` 均为原子 temp + rename、文件 `0600`
5. **现代 Go**：遵循 JetBrains `use-modern-go`（Go 1.26）

## 请求生命周期（chat）

1. API Key 校验（`RequireAPIKey` 为真时）
2. 解析 model → 上游 ID（剥离 `codebuddy/` · `codebuddy:` 前缀，空归一为 `auto`）
3. 从账号池选号（按当前号池 site 过滤；`ExcludeIDs` 排除已试账号）。**同一会话钉在第一次成功的账号**上；无钉或钉已失效时，用缓存的剩余额度 `PreferQuota` 选最大者（无线额视为极大）。新会话会并行补探 **缺失或超过 5 分钟** 的额度快照（只打过期的号，整批最多 2.5s）；快照仍新鲜则不打 billing
4. 必要时 refresh token（默认提前 10 分钟窗口，鉴权失败可强制刷新）
5. 组装 protocol_direct headers + body（**端点以账号 site 为准**）
6. 非流式：聚合为 JSON；流式：立即开 SSE + keep-alive + 增量 chunk
7. 收尾写 usage chunk，回写账号统计、全局 stats，并追加 `usagejournal` 明细（可选落盘）

**失败处理**：

- 鉴权类失败 → 强制 refresh 后重试同一账号一次
- **429·502·503·504·rate limit / 额度耗尽** 等可恢复上游故障 → 松开该会话的账号钉，标记 `failedRequests` + `lastError`，写入 `cooldownUntil`；对仍可用的候选账号**并行探活额度**（只打 `/billing/meter/get-user-resource`，整批最多 2.5s，失败或超时保留缓存），再按 `PreferQuota` 选剩余最大者，用 `ExcludeIDs` 换号重试（单请求最多 16 个账号）。新会话的探活仍按第 3 步：只补 **缺失或超过 5 分钟** 的快照，快照新鲜则不打 billing。号池试尽或触达上限时回传真实上游错误，不用「重试深度超限」掩盖 429/503
- 同区域**全部账号冷却** → 降级选 `cooldownUntil` 最小者（避免整体不可用）
- `11140` / `11128` / `11101` / `11102` → **不换号**；失败仍写入冷却
- 客户端主动取消 → 按正常结束计，不计失败

## 并发模型

| 机制 | 位置 | 说明 |
|------|------|------|
| `atomic.Pointer[config.Config]` | `gateway.Service` | 运行时配置唯一快照，改 Key / 号池不重启 |
| 值快照 | `LiveOAuthSession()` / `CurrentOAuth()` | 不外泄可变 `*OAuthSession`，避免 data race |
| 锁内鉴权 | `OAuthLaunchAuthorized(id, token)` | launch / callback 在锁内比对，杜绝并发重置竞态 |
| singleflight | `modelsFlight` | 模型列表并发 cache miss 合并回源 |
| 常量时间比较 | `crypto/subtle` | API Key / admin 密码比对 |
| 异步刷盘 | `accounts.Pool.flushLoop` | 250ms 合并写，退出时强制 flush |

## 账号池持久化

- `Select` / `MarkResult` 只改内存并标 dirty，由后台 goroutine 合并落盘
- OAuth / Upsert / Delete / Replace / SetEnabled 同步刷盘（凭据不可丢）
- 文件权限 `0600`，写入走 temp + rename 原子替换
- `Pool.Close` / `Service.Close` / `Server.Shutdown` 强制 flush

## 与 Node legacy 的关系

Go 子项目是唯一实现。新功能不要回写 Node，也不要恢复多传输默认路径。
