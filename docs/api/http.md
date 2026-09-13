# HTTP API

所有 `/v1` 接口要求 Header：

```http
Authorization: Bearer <CODEBUDDY_PROXY_API_KEY>
```

也接受 `X-API-Key` 作为备选 Header（二者任一匹配即可）。
当 `CODEBUDDY_PROXY_REQUIRE_API_KEY=false` 时 `/v1` 免鉴权。

鉴权失败统一返回：

```json
{"error":{"message":"Missing or invalid API key","type":"authentication_error"}}
```

> Key 比对使用 `crypto/subtle` 常量时间比较。

---

## Public

### `GET /health` · `HEAD /health`

无需鉴权。

```json
{"ok":true,"provider":"codebuddy","transport":"protocol_direct","version":"v4.8"}
```

`version` 为构建时注入的发布号；本地 `go build` 未带 `-ldflags` 时多为 `dev`。

### `GET /v1/models`

别名：`GET /models`

Query：

| 参数 | 说明 |
|------|------|
| `fresh` | `1` / `true` / `yes` / `on` 强制回源，忽略 60s 缓存 |

返回 OpenAI `list` 形状。模型来源优先级：上游 `/v3/config` → 配置中的 `CODEBUDDY_PROXY_MODELS`（默认 `auto`）。

```json
{
  "object": "list",
  "data": [
    {
      "id": "auto",
      "object": "model",
      "created": 1787986908,
      "owned_by": "codebuddy",
      "credits": "1",
      "credit_multiplier": 1,
      "free": false,
      "description": "...",
      "context_length": 1000000,
      "max_input_tokens": 1000000,
      "max_output_tokens": 50000,
      "limit": { "context": 1000000, "output": 50000 }
    }
  ]
}
```

`credits` / `credit_multiplier` / `free` / `description` 为可选字段，仅当上游提供时出现。

`context_length` / `max_input_tokens` / `max_output_tokens` / `limit` 透传上游 `/v3/config` 的 `maxInputTokens`（缺省回落 `maxAllowedSize`）与 `maxOutputTokens`。客户端应使用这些值作为上下文窗与输出预算，而不是把 1M 窗口再按比例预留一大块输出（例如 DeepSeek V4 Flash 上游输出上限是 5 万）。

CLI 目录里部分思考模型（尤其是 DeepSeek Flash）会省略 `canDisableThinking` / `supportedEfforts`。代理会再用 VSCode 头拉一次 IDE `/v3/config`，把这两项以及 `defaultEffort` 补进 `reasoning_config`（不覆盖 CLI 已有值）；若仍缺失 `canDisableThinking`，则默认 `true`。这样 `/v1/models` 才会带上 `variants.none` 与 `reasoning_options` 的 toggle，避免客户端把 Flash 当成「思考始终开启」并预留约 30% 窗口。

单一模型查询 `GET /v1/models/{id}` **不支持**，会返回 404 `not_found_error`。请拉取列表后在客户端侧匹配 `id`。

**模型缓存**：默认 60s TTL。切换号池区域会自动失效；管理台「刷新模型」走 `fresh=true`。
并发 cache miss 由本地 singleflight 合并，不会打爆上游 `/v3/config`。

### `GET /v1/model/info`

别名：`GET /model/info`

供 **OpenCode `opencode-models-discovery`** 插件使用（`modelInfoFormat: "litellm"`）。

Query：

| 参数 | 说明 |
|------|------|
| `fresh` | 同 `/v1/models`，强制回源 |

返回 LiteLLM enricher 形状（`data[]` 每项含 `model_info.max_input_tokens` / `max_output_tokens` / `max_tokens`，以及 `supports_reasoning` 等）：

```json
{
  "data": [
    {
      "model_name": "glm-5.3-flash",
      "litellm_params": { "model": "glm-5.3-flash" },
      "model_info": {
        "key": "glm-5.3-flash",
        "mode": "chat",
        "max_input_tokens": 1000000,
        "max_output_tokens": 32000,
        "max_tokens": 32000,
        "supports_reasoning": true
      }
    }
  ]
}
```

OpenCode 配置示例：

```json
{
  "plugin": ["opencode-models-discovery"],
  "modelInfoFormat": "litellm",
  "modelInfoEndpoint": "/model/info"
}
```

> `/v1/models` 已含 `reasoning` / `variants` 等 OpenCode 字段；discovery 插件仍需本端点做 LiteLLM 形态 enrich。

### `POST /v1/chat/completions`

别名：`POST /chat/completions`

```json
{
  "model": "auto",
  "stream": true,
  "messages": [{"role": "user", "content": "hello"}],
  "temperature": 0.7,
  "top_p": 0.9,
  "max_tokens": 4096,
  "tools": [],
  "tool_choice": "auto"
}
```

| 字段 | 说明 |
|------|------|
| `model` | 支持 `codebuddy/<id>` / `codebuddy:<id>` 前缀，会被剥离为上游 ID；空或 `default` 归一为 `auto` |
| `stream` | `false` → `application/json`；`true` → `text/event-stream` |
| `max_tokens` / `max_completion_tokens` | 二者取正数，后者优先 |
| `tool_choice` | 对象型会被归一为 `auto` / `none` / `required` |

JSON 请求体上限 **64MiB**（`httputil.MaxJSONBodyBytes`）。超过时返回 `413`：

```json
{"error":{"message":"Request body exceeds 64MB. 1M-context requests need a larger JSON body.","type":"invalid_request_error"}}
```

非法 JSON 仍为 `400` `Invalid JSON body`。

**流式行为**：

1. 连接建立后**立即**发送 SSE 头与首个 `role=assistant` chunk，客户端不会把上游 TTFB 误判为挂起
2. 期间按 `CODEBUDDY_PROXY_STREAM_KEEPALIVE_MS`（默认 5000ms）发送 `: keep-alive` 注释
3. 结束时发送 finish chunk（`stop` 或 `tool_calls`）
4. 再发一条 `stream_options.include_usage` 风格的 usage chunk（`choices: []`）
5. 最后 `data: [DONE]`

usage chunk 形如：

```json
{
  "id": "chatcmpl_codebuddy_...",
  "object": "chat.completion.chunk",
  "model": "auto",
  "choices": [],
  "usage": {
    "prompt_tokens": 12,
    "completion_tokens": 34,
    "total_tokens": 46,
    "prompt_tokens_details": {"cached_tokens": 8},
    "cache_read_input_tokens": 8
  }
}
```

缓存字段同时兼容 Anthropic / DeepSeek 别名（`cache_read_input_tokens` / `cache_creation_input_tokens` / `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`）。

**流式中的错误**：若首字节未发出，返回 `502` + JSON error；若已开始流式，则在 SSE 内写入 `{"error":{...}}` 后再 `[DONE]`，避免客户端挂死。

客户端主动断开（`context canceled`）按正常结束计，不计入失败。

---

## Admin UI

- `GET|HEAD /direct-admin` 与 `/direct-admin/`

鉴权（满足任一即可）：

- **免密**：`CODEBUDDY_PROXY_ADMIN_PASSWORD` 为空时直接放行（本地推荐）
- **Basic Auth**：用户名 `admin` 或留空，密码为 admin 密码
- **Bearer**：`Authorization: Bearer <admin 密码或 API Key>`

> 已移除 `?password=` query 认证——密钥会泄进代理日志、浏览器历史与 Referer。

---

## Admin API

统一前缀 `/direct-admin/api/`。鉴权同 Admin UI。

**CSRF**：跨域的写请求（非 GET/HEAD，`Origin` 与本站不一致）返回 `403`：

```json
{"ok":false,"error":"cross-origin admin mutation blocked"}
```

### 运行态

| Method | Path | 说明 |
|--------|------|------|
| GET | `/direct-admin/api/status` | 运行状态 + 账号摘要 + 配置快照；含 `version`、`build`（`version` / 可选 `commit` / `builtAt`）、进程级 `stats` |
| GET | `/direct-admin/api/usage` | 用量汇总 + 分页明细 + 趋势 `series`；默认落盘 `proxy-usage.json`（约 400 条环形缓冲 + 90 日汇总） |
| GET | `/direct-admin/api/client-config` | 前端配置（baseUrl / apiKey / site / requireApiKey） |
| POST | `/direct-admin/api/client-config/generate-key` | 生成 `cbp_...` Key，写入 `~/.codebuddy/proxy.env`（或已有 `.env`）并立即生效 |
| POST · PUT | `/direct-admin/api/pool-site` | 切换号池区域 `domestic` / `global`，回写 `.env` |
| POST · PUT | `/direct-admin/api/pool-product` | 切换上游产品 `codebuddy` / `workbuddy`，回写 `.env`；国内国际账号共用同一选择 |

`generate-key` 会同步置 `CODEBUDDY_PROXY_REQUIRE_API_KEY=true`。**旧 Key 立即失效**，客户端必须同步更换。

### 账号池

| Method | Path | 说明 |
|--------|------|------|
| GET | `/direct-admin/api/codebuddy/status` | 账号池摘要 |
| GET | `/direct-admin/api/codebuddy/accounts` | 账号列表 |
| DELETE | `/direct-admin/api/codebuddy/accounts/{id}` | 删除账号 |
| POST | `/direct-admin/api/codebuddy/accounts/{id}/enable` | 启用 |
| POST | `/direct-admin/api/codebuddy/accounts/{id}/disable` | 禁用 |
| GET | `/direct-admin/api/codebuddy/accounts/{id}/usage` | 拉取该账号 Credits（剩余/总额） |
| POST | `/direct-admin/api/codebuddy/accounts/{id}/refresh-token` | 强制刷新 token |
| POST | `/direct-admin/api/codebuddy/checkin` | 对指定号池内已启用账号批量每日签到；body 可选 `{"site":"domestic"}`，省略时跟随当前激活号池（`CODEBUDDY_SITE`） |

`checkin` 响应示例：

```json
{
  "ok": true,
  "poolSite": "domestic",
  "note": "国内站每日签到约 100 积分，连续第 7 天可达 1000 积分。",
  "summary": {
    "total": 2,
    "checkedIn": 1,
    "unsupported": 1
  },
  "results": [
    {
      "ok": true,
      "accountId": "…",
      "site": "domestic",
      "supported": true,
      "rewardCredits": 100,
      "streakDays": 1,
      "message": "签到成功，+100 积分"
    }
  ]
}
```

响应字段采用 `omitzero` 序列化（`coding-standards` 要求）：`ok` / `supported` / `alreadyCheckedIn` 等 bool 与 `rewardCredits` / `streakDays` 等数值为 `false` / `0` 时不出现在 JSON 里，`summary` 中计数为 0 的项同样省略（上例已按线上形态省略零值）。管理台按 truthy 读取，缺失与 `false` / `0` 等价；若你在脚本里断言字段存在，请先判空。

字段说明：`supported` 缺省表示上游 `active:false` 或活动未开启；`alreadyCheckedIn=true` 含上游 `code=10001` 幂等；网络/鉴权失败时 `supported=true` 且计入 `summary.failed`。单账号总计超时 20s（含状态查询 + 签到）；批次总上限 5 分钟（约 `20s × 账号数`）。

`site` 缺省时取当前激活号池（`ActivePoolSite()`，即 `CODEBUDDY_SITE`），与 `/direct-admin/api/status`、`models` 保持一致；显式传 `site` 时才覆盖（`cn` 等别名归一化为 `domestic`）。批次 deadline 耗尽时，未执行的账号计入 `summary.skipped` 并附 `message`（不会发起注定超时的请求），批次 `ok` 仍为 false。

账号动作未知时返回 `404` + `{"ok":false,"error":"unknown account action"}`。

### 模型

| Method | Path | 说明 |
|--------|------|------|
| GET | `/direct-admin/api/codebuddy/models` | 模型列表，默认 `fresh=true`；`?fresh=0` 走缓存 |
| POST | `/direct-admin/api/codebuddy/probe` | 强制回源探测模型（等价 `fresh=true`） |

### OAuth

| Method | Path | 说明 |
|--------|------|------|
| POST | `/direct-admin/api/codebuddy/oauth/start` | 开始 OAuth，body 可带 `site` / `label` / `reuseExisting` |
| POST | `/direct-admin/api/codebuddy/oauth/poll` | 轮询登录结果 |
| POST | `/direct-admin/api/codebuddy/oauth/callback` | 同 poll（回调页用） |
| GET | `/direct-admin/api/codebuddy/oauth/session` | 当前会话状态 |
| GET · HEAD | `/direct-admin/codebuddy/oauth/launch` | 浏览器登录入口，`?id=&token=` 鉴权后 302 到上游 |
| GET · HEAD | `/direct-admin/codebuddy/oauth/callback` | OAuth 回调落地页 |

`start` 的 `reuseExisting=true` 且会话仍在 15 分钟 TTL 内时，复用现有 `waiting` 会话而不重新发起。

launch / callback 均设 Cookie `cursor_codebuddy_oauth=<token>`（`HttpOnly`、`SameSite=Lax`、`Max-Age=900`）。

### 用量与明细

`GET /direct-admin/api/usage`

Query：

| 参数 | 说明 |
|------|------|
| `range` | `day`（默认）· `week` · `month`（不足 30 天历史时与 `week` 同窗口） |
| `account` | 可选，按 `accountLabel` 或 `accountId` 筛明细与汇总 |
| `model` | 可选，按模型名筛明细与汇总 |
| `limit` | 每页条数，默认 `20`，最大 `100` |
| `offset` | 跳过条数（与 `page` 二选一，默认 `0`） |
| `page` | 页码，从 `1` 起；等价 `offset = (page-1)*limit` |

明细按时间**新→旧**；响应含 `requestsTotal`、`limit`、`offset`、`byModel`（窗口内按模型命中率，不受 `account`/`model` 筛选影响）、`accounts`、`models`。

响应：

```json
{
  "ok": true,
  "summary": {
    "range": "day",
    "from": 1757606400000,
    "to": 1757692800000,
    "requests": 12,
    "failed": 1,
    "promptTokens": 48000,
    "completionTokens": 3200,
    "totalTokens": 51200,
    "cachedTokens": 12000,
    "cacheHitRate": 25.0,
    "credits": 0.84,
    "creditRows": 10
  },
  "series": [
    {"at": 1757691000000, "label": "14:00", "totalTokens": 12000, "cacheHitRate": 30.5}
  ],
  "requests": [
    {
      "at": 1757691000000,
      "proxyRequestId": "a1b2c3…",
      "upstreamConversationRequestId": "…",
      "upstreamConversationId": "…",
      "upstreamMessageId": "…",
      "sessionLabel": "修复登录 bug",
      "model": "auto",
      "stream": true,
      "ok": true,
      "promptTokens": 1200,
      "completionTokens": 80,
      "cachedTokens": 900,
      "totalTokens": 1280,
      "credit": 0.07,
      "durationMs": 2340,
      "accountId": "…",
      "accountLabel": "CodeBuddy OAuth"
    }
  ]
}
```

- 默认落盘 `proxy-usage.json`（与 `proxy-accounts.json` 同目录，可用 `CODEBUDDY_PROXY_USAGE_PATH` 覆盖）：最多约 **400** 条明细环形缓冲 + **90 日**按日汇总；正常退出与防抖写入（约 250ms）会刷盘，`SIGKILL` 可能丢最近未落盘的几条。
- `credit` 仅在上游 chat `usage` 返回 `credit` 时写入；无字段时不估算。与套餐 `/billing/meter/get-user-resource` 的余额无关。
- 客户端主动断开（`context canceled`）时与全局统计一致：记为 `ok: true`、token 常为 0，不算 `failed` 计数。

管理台 Tab **05 / 用量与明细** 与 `#usage` 锚点调用本接口。

---

## CORS

`OPTIONS` 响应：

```http
Access-Control-Allow-Origin: *
Access-Control-Allow-Headers: Authorization, Content-Type, X-API-Key
Access-Control-Allow-Methods: GET,POST,DELETE,OPTIONS
```

`/v1/models` 与非流式 `/v1/chat/completions` 的成功响应也带 `Access-Control-Allow-Origin: *`。

---

## 未匹配路由

兜底返回 `404`：

```json
{"error":{"message":"Unsupported route: /v1/models/auto","type":"not_found_error"}}
```
