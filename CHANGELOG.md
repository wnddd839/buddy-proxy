# 更新日记

> CodeBuddy Proxy（Go · `protocol_direct`）发布说明。  
> 仓库：[`wnddd839/buddy-proxy`](https://github.com/wnddd839/buddy-proxy)

---

## v4.0 · 2026-09-09 · 号池配额冷却与 Go 1.26 现代化

### 号池 / 计费

- **配额感知选号**：缓存上游 billing 配额到账号；配额耗尽时在冷却窗口内跳过该账号，避免无效轮询。
- **配额错误探测**：上游返回配额类错误时主动拉取 billing usage，用 `resetAt` 对齐账号 `cooldownUntil`。
- **重试深度**：`completeRetryLimit` 随当前区域活跃账号数缩放（上限 16），多账号代理下减少过早放弃。
- **管理台**：账号卡片展示配额耗尽与恢复时间（`quotaExhausted` / `quotaResetAt`）。

### 工程

- Go 1.26 现代化：`new(expr)`、`sync.OnceValue`（billing 时区）、内建 `min`/`max`、`slices.Contains`、`t.Context()`、`QuotaState` 字段 `omitzero`。
- 删除未使用的 `gateway.Service.ApplyQuotaFromUsage` / `SyncAccountQuota`，server 层直接编排 `QuotaStateFromUsage` + `Pool.ApplyQuotaState`。
- 补 `QuotaState` JSON `omitzero` 表驱动测试，锁定 admin usage API 的 `quota` 字段序列化行为。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.0

---

## v0.3.10 · 2026-09-05 · 上游 11128 排障与小规模适配

### 根因

- ZCode agent 每次请求在 `system` 中注入 `System Context` 块（含 `Main branch (you will usually use this for PRs): <branch>`），腾讯上游 `copilot.tencent.com` 将该整串判为非官方 CLI 通道，返回 `400 + 11128 Illegal API invocation from an unapproved channel`。经二分验证：该三件套（`Main branch` + 括号注解 + 冒号）齐备即触发，缺任一件即放行；git 状态文本本身、`role=developer`、工具数、`thinking`、`max_completion_tokens` 均已证伪。

### 排障

- 上游非 2xx / 流中 `upstream_error` 时进程 WARN 日志 `codebuddy upstream failure` 输出脱敏请求指纹（role 序列 / 工具名 / thinking / reasoning / 单消息 200 字预览，不含 token 与完整 prompt），`docs/operations/runbook.md` 同步。
- `gateway.New` 向 provider 注入进程 logger，避免与 `slog.Default()` 错配。

### 适配

- `system` 消息去除 ` (you will usually use this for PRs)` 括号注解（保留分支名，`Main branch: <branch>`），用户原文与工具结果原样透传。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.10

---

## v0.3.9 · 2026-09-03 · 管理台章节切页

### 管理台

- **Editorial 四章节切页**：概览与监控 / 账号池与授权 / 客户端接入 / 模型与快照，避免单页堆叠。
- URL hash 联动：`#codebuddy` → 账号池与授权；`#client-config` → 客户端接入（OAuth 返回路径可用）。
- 概览说明与号池冷却语义对齐（429 → 2min，111xx → 5min，全冷却降级选最早恢复者）。
- DOM id / JS API / 后端协议仍零变更。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.9

---

## v0.3.8 · 2026-09-03 · Editorial 视觉改版

### 视觉与排版

- **Editorial 杂志设计系统落地**：管理台（`direct-admin`）、OAuth 回调落地页（`LaunchPage`）与 GitHub Pages 产品页全面采用暖奶油 `#F9F8F6` + 软黑 `#1C1C1C` + 单色透明度层级，移除全部过时彩色光晕、噪点层、Canvas 氛围层与 Web Font CDN 依赖。
- **Logo 单色化**：`docs/logo.svg` 重写为印刷风格纯单色线标（折线汇入、水平出线），与全站 Editorial 视觉语言一致。
- **设计规格**：`design/` 目录改为 Editorial 唯一源；旧深色仪器风 / 粒子环境层规格作废。
- **DOM 契约保持**：管理台 id / JS 行为 / 后端 API / 账号池协议零回归；仅展示层变更。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.8

---

## v0.3.7 · 2026-08-31 · 号池冷却与规范落地

### 号池冷却

- 上游失败写入账号 `cooldownUntil`（11140/11128/11101/11102 → 5min，429 → 2min，502/503/504 → 30s），冷却期内不参与轮询。
- **全冷却降级**：同区域全部账号冷却时，选 `cooldownUntil` 最小者继续服务（debug 日志 `pool select bypassed cooldown`），避免整体不可用。
- **11140** 不再触发换号重试（策略错误换号无意义）。

### OAuth / 账号

- **Upsert 去重**改为 `(userId + site)`：国内/国际 OAuth 可并存。若旧版已合并为单账号，升级后需对缺失区域 **重新 OAuth**（不会自动拆分）。

### 路由与文档

- 补注册 **`GET /model/info`**（与 `/v1/model/info` 等价，OpenCode discovery 配置可用 `/model/info`）。
- `docs/api/http.md` · runbook WARN 级别 · architecture 失败处理表同步。

### 工程规范

- `slices.Contains`、`new(loggedIn)`；`.githooks/pre-commit`（gofmt 检查 + 受影响包测试，不自动改暂存）。
- Pi：`.agents/pi-system-prompt-go.md` + skill `go-codebuddy-modern`。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.7

---

## v0.3.6 · 2026-08-31 · OpenCode / 号池 / 国际模型列表

### OpenCode 思考档位透传

- **根因**：OpenCode `@ai-sdk/openai-compatible` 需要 models.dev 形态字段（`reasoning: true` 布尔、`reasoning_options`、`variants`），此前 `/v1/models` 把上游 `reasoning` 对象直接透出，客户端无法识别思考档位 UI。
- **`GET /v1/models` 扩展**：支持思考的模型返回 `reasoning: true`、`reasoning_config`（上游元数据）、`reasoning_options`、`interleaved`、`variants`（按 `supportedEfforts` 生成，如 `glm-5.3-flash` 为 `low/high/max`）。
- **`GET /v1/model/info`**：LiteLLM 格式元数据，供 `opencode-models-discovery` 配置 `modelInfoFormat: "litellm"` 自动注入思考档位（provider 级一次配置，无需 per-model 手配）。
- **管理台**：不增加「复制 opencode.json」入口；透传 + discovery 插件即可。

### 号池 failover

- **429 / 502 / 503 / 504 / rate limit** 等可恢复上游故障：标记当前账号 `failedRequests` + `lastError`，自动换下一个号重试（最多 3 层，轮询选号 + `ExcludeIDs` 跳过已试账号）。
- **仍不换号**：`11128`（渠道限制）、`11101` / `11102`（请求/模型形态）、`401`/`403`（先 OAuth refresh 同一账号）。

### 国际站模型列表（hy4 缺失）

- **根因**：国际号池只打 `www.codebuddy.ai/v3/config`（约 35 个模型，含 Gemini/GPT，**无 hy4**）；`copilot.tencent.com/v3/config` 用同一国际 token 可读且含 `hy4-preview` 等。
- **修复**：国际站合并两个 `/v3/config` 源（去重合并）；国内站仍只走 `copilot.tencent.com`。聊天仍按账号区域走 `codebuddy.ai` / `copilot.tencent.com` 对应 chat 端点。

### 二进制启动体验

- 启动时输出简短中文提示，**引导打开管理台**完成 OAuth 与客户端接入配置。
- 默认日志级别 `warn`（排障设 `CODEBUDDY_PROXY_LOG_LEVEL=info|debug`）；**不再在日志中打印明文 API Key**。
- 号池切换日志降为 debug。

### 工具

- `go-codebuddy/scripts/probe-v3-models.js`：对比各上游 `/v3/config` 模型列表（排障用）。

### Release 资产命名

- **Windows 64 位请下载 `codebuddy-proxy-windows-x64.exe`**（与 `windows-amd64.exe` 相同，后者为兼容旧名）。
- 勿在 Windows PC 上下载 `darwin-arm64` / `darwin-amd64`（macOS 专用）。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.6

---

## v0.3.5 · 2026-08-31 · 思考档位透传

- **Chat 入站**：解析 `reasoning_effort` / `reasoningEffort` / `reasoning` / `thinking`，写入上游 body。
- **上游对齐**：实测仅 `reasoning.effort` 触发 `reasoning_content`；网关将 `reasoning_effort` 映射为 `reasoning: { effort }`（同时保留 `reasoning_effort` 兼容字段）。
- **模型列表**：`/v1/models` 透传 `supportsReasoning` / `onlyReasoning` / `reasoning`（含 `supportedEfforts`、`defaultEffort`）。
- **响应**：流式 delta 与 non-stream `message.reasoning_content` 回传思考内容；无思考时 `omitempty` 零回归。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.5

---

## v0.3.4 · 2026-08-30 · 出站 cache 字段别名补齐

- **问题**：管理台能看到 `totalCachedTokens`，但 CCSwitch 等下游缓存率仍极低。
- **核实**：流式收尾 `usage` chunk（空 choices）本身已在发送；CodeBuddy 上游多用 DeepSeek 风格 `prompt_cache_*` 字段。
- **修复**：`UsageFromProvider` 在命中时同时写出 `prompt_tokens_details.cached_tokens` + `prompt_cache_hit_tokens` + `cache_read_input_tokens`，并推导 `prompt_cache_miss_tokens`；`ParseUsage` 兼容 `cached_tokens` / `input_tokens_details`。
- **流式**：usage chunk 写出后显式 `Flush()`，降低下游提前断开丢统计的概率。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.4

---

## v0.3.3 · 2026-08-29 · 回滚出站混合序列化（热修复）

- **回滚**：删除 `MarshalStreamChunk` / `SSEMarshaler` 手写骨架路径，出站 `StreamChunk` 恢复 `json.Marshal`。
- **原因**：typed struct 的 `json.Marshal` 已是 **2 allocs / ~288B**；混合路径 **9 allocs / ~552B**，实测为负优化（上轮基线误用 `map[string]any`）。
- **保留**：v0.3.1 SSE 缓冲聚合、v0.3.0 上游 typed 解析、中文 token 估算等有效优化不受影响。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.3.3

---
