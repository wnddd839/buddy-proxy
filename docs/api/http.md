# HTTP API

所有 `/v1` 接口要求 Header：

```http
Authorization: Bearer <CODEBUDDY_PROXY_API_KEY>
```

也接受 `X-API-Key` 作为备选 Header（二者任一匹配即可）。
当 `CODEBUDDY_PROXY_REQUIRE_API_KEY=false` 时 `/v1` 免鉴权。

Chat、Responses、Images JSON、搜索、网页抓取和 agentic search 的 JSON 请求体默认最多 64 MiB，可通过 `CODEBUDDY_PROXY_MAX_BODY_MB` 调整；超限返回 413，错误文案显示实际上限。别名路由共用该限制；管理接口和图片 multipart 限制不变。

Chat 普通号池的当前模型全部处于 6004 / rate-model 冷却时，不再向这些账号发请求；非流式错误提供 `Retry-After` 响应头与 `error.retry_after`（秒），已打开的流提供 SSE `error.retry_after`。保留既有 HTTP 错误状态约定；显式固定账号 Key 仍可直接请求该账号。

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
{"ok":true,"provider":"codebuddy","transport":"protocol_direct","version":"v0.4.9.4"}
```

`ok` 表示**进程存活**（liveness）。上游挂掉时这里仍是 200。

Query：

| 参数 | 说明 |
|------|------|
| `deep=1` | 附带上游可达性探测（有 20s 缓存）。上游不可达时返回 **503** `ok:false` |

探测向 chat 端点发空 POST（无 token）：**401** 视为鉴权层可达，**502/504 + HTML** 视为上游基础设施故障。同 IP 高频探测会触发 WAF，因此必须走缓存。

### `GET /readyz` · `HEAD /readyz`

无需鉴权。`ok` 为 **anyOK**：任一探测区域可达即 200。两区都要活时请看 `upstream.allOk`。

```json
{"ok":false,"upstream":{"ok":false,"allOk":false,"probed":true,"cause":"upstream_infra","sites":{"global":{"ok":false,"status":502}}}}
```

探测缓存按 `site|product` 分区；管理台切换 CodeBuddy/WorkBuddy 或默认 SITE 会立即清空，避免把旧产品的 502/可达带到新产品。

`version` 为构建时注入的发布号；本地 `go build` 未带 `-ldflags` 时多为 `dev`。

### `GET /v1/models`

别名：`GET /models`

Query：

| 参数 | 说明 |
|------|------|
| `fresh` | `1` / `true` / `yes` / `on` 强制回源，忽略 60s 缓存 |

返回 OpenAI `list` 形状。模型来源优先级：上游控制台目录 `/console/enterprises/personal/models`（对话可用模型，带 `credits`）拼合 **CLI 身份** `/v3/config` 补缺（console 元数据优先，例如 `glm-5.0-turbo` / `minimax-m2.7` / `hunyuan-image-alpha-edit` 只出现在 CLI 目录）→ console 失败时回落 `/v3/config` → 配置中的 `CODEBUDDY_PROXY_MODELS`（默认 `auto`）。不并 IDE/WorkBuddy 目录，避免混入 `codewise-*`。补缺只加新 id，不覆盖 console 已有行（`default` 与 CLI `auto` 同为对外 `auto` 时保留 console 行，避免丢 `credits`）。Desktop 试用别名（如 `hy4-preview-f`）在上游 Console 目录里就没有，因此也不出现在结果里。

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

### `POST /v1/responses`

别名：`POST /responses`

OpenAI **Responses API** 协议翻译层，主要面向 Codex CLI 等以 `/v1/responses` 接入的客户端。请求在入口翻译为 Chat Completions 打到上游号池，响应/流式事件再翻译回 Responses 形态。

**已支持：**

- `input`（字符串或 item 数组：`message` / `function_call` / `function_call_output`；`reasoning` item 接收但不回放）、`instructions`（→ system 消息）、`developer` role（→ `system`）。历史 `function_call` / `function_call_output` 译成 chat 的 `assistant.tool_calls` + `tool`，与上游配对。
- `tools`（Responses 扁平形态 `{type:"function", name, parameters}` 与 chat 嵌套形态均可）、`tool_choice`（`auto`/`none`/`required`/`{type:"function",name}`）
- **内置 `web_search` / `web_search_preview`**：代理把工具桥接成保留内部名称的 function，避免与客户端自己定义的 `web_search` function 冲突；服务端用当前账号的官方搜索接口执行并回填结果，默认只执行一轮搜索。
- `reasoning.effort`、`max_output_tokens`、`temperature`、`top_p`、`prompt_cache_key`（→ 会话钉号；同会话同时复用上游 `X-Conversation-ID`）
- 非流式：返回完整 `response` 对象（`output[]` 含 `reasoning` / `message` / `function_call` item + `usage`）
- 流式：命名 SSE 事件（`event: response.output_text.delta` 等），完整生命周期 `response.created → output_item.added → *.delta → output_item.done → response.completed`；输出被截断时终止事件为 `response.incomplete`。含 `response.reasoning_summary_text.delta`（思考）与 `response.function_call_arguments.delta/done`（工具调用），`sequence_number` 单调递增

**不支持（明确降级）：**

- `background: true` → `400`
- `previous_response_id` / `item_reference` / `store`：无服务端状态存储，不回放历史 output；`previous_response_id` 在缺少 `prompt_cache_key` 时仅用作会话钉键（Codex `store:false` 本就不依赖服务端存储）
- `file_search` / `computer_use` 等其他内置工具：静默丢弃
- 图像输入（`input_image`）：忽略

#### 内置联网搜索

调用链：**模型发起 `web_search` → 代理用当前账号的官方 `/agenttool/v1/search` 检索 → 结果作为工具回执回填 → 模型作答**。

- 只对声明了 `{"type":"web_search"}`（或 `web_search_preview`）的请求生效；没有声明时完全不触发。
- `tool_choice: "none"` 优先，即使声明了内置搜索也不提供或执行搜索工具。
- 搜索使用当前请求区域的账号池 token，沿用账号的官方搜索配额及计费规则。`15001` 为搜索配额相关错误；尚未进行账单前后对账，不据此推定单次积分扣费。代理不接独立的第三方搜索服务。
- 每次请求最多执行一轮搜索，回填后第二次模型调用会移除 `web_search` 工具，避免重复扣费与死循环。
- 模型在同一轮里同时调用 `web_search` 与客户端 function 时，代理只执行搜索；客户端 function 调用会被丢弃，模型可在拿到搜索结果后重新发起。
- 搜索失败不会让整轮对话失败：错误文本会作为工具回执回填，由模型决定改述或放弃。
- `stream=true` 会立即发送 created/in_progress 和保活；最终模型答案仍先聚合后以 SSE 回放，不是逐 token 实时转发。返回的模型 usage 汇总首轮及回填后的末轮；上游缺失的搜索 Credit 不估算。

```json
{
  "model": "auto",
  "input": "今天有什么值得关注的科技新闻？",
  "tools": [{"type": "web_search"}],
  "stream": false
}
```

错误形态为 Responses 风格 `{"error":{"code":"...","message":"..."}}`。

输出预算：`max_output_tokens` 转成上游实际识别的 `max_tokens`，并同时发送同值的 `max_completion_tokens`。上游以 `length` / `max_tokens` 结束时，返回 `status:"incomplete"` 和 `incomplete_details:{"reason":"max_output_tokens"}`，保留已生成的内容与用量；流式发送 `response.incomplete`，不伪装成完整完成。内容过滤对应 `reason:"content_filter"`。不指定正数上限时不发送这两个限制字段。

图片支持：`message.content` 中的 `input_image` 转为 Chat 的 `image_url` 内容块；`function_call_output.output` 接受带图片的结构化数组，也识别工具将该数组编码成 JSON 字符串的形态。保留文本和 `tool_call_id` 关联，图片放在同组连续工具结果全部结束后的 user 消息中，避免把图片 base64 当普通文本计入上下文，或在并行工具回执中间插入用户消息。普通 JSON 工具输出不做图片协议推断。

图片内容由上游验证；代理不下载远程 URL，也不全量解码图片。模型必须支持视觉，超出上游图片或上下文限制仍会返回错误，不保证任意载荷均可发送。

### `POST /v1/images/generations`

别名：`POST /images/generations`

上游 `POST /v2/images/generations`。目录里 `tags` 含 `text-to-image` 的模型（当前实测：控制台 `hunyuan-image-alpha`，CLI `/v3/config` 补进的 `hunyuan-image-alpha-edit` / `hunyuan-image-v3.0-art`）走这里，不要打 chat。

```json
{
  "model": "hunyuan-image-alpha",
  "prompt": "a red circle",
  "n": 1
}
```

成功返回 OpenAI Images 形状：`data[].url`（上游 COS 链接）加 `usage`。上游始终只回一张图，且不接受 `b64_json`。`n` 缺省或 `1`；其他值与 `response_format=b64_json` 返回 `400`。`size` 上游会忽略，代理不转发。

### `POST /v1/images/edits`

别名：`POST /images/edits`

上游 `POST /v2/images/edits`。必须带参考图：

- `image`：URL、base64，或 `data:` URL（只取逗号后的 base64）。可以是字符串或数组。
- `image_url`：字符串或字符串数组。上游字段是 `[]string`，代理会把单值包成数组。

缺参考图返回 `400`。响应形状与 generations 相同。

### `POST /v1/search`

别名：`POST /search`

官方关键词搜索。上游 `POST /agenttool/v1/search`，使用账号池中当前区域账号的 OAuth token，沿用官方账号的搜索配额及计费规则；单次积分扣费尚未做账单对账。

```json
{
  "model": "auto",
  "query": "腾讯 2026 第三季度 财报",
  "max_results": 5,
  "allowed_domains": ["tencent.com"],
  "freshness": "m3"
}
```

| 字段 | 说明 |
|------|------|
| `query` | 必填，非空搜索词 |
| `max_results` | 可选，`1`–`50`，缺省上游默认 `5` |
| `allowed_domains` | 可选，域名白名单 |
| `blocked_domains` | 可选，代理按官方客户端行为拼成 `-site:` 排除项 |
| `freshness` | 可选，`d1`–`d30` / `m1`–`m12` / `y1`–`y5` |
| `model` / `X-Site` | 与 chat 相同的区域选择；实际不切换模型 |

成功响应：

```json
{
  "object": "search.result",
  "query": "腾讯 2026 第三季度 财报",
  "search_type": "text2text",
  "provider": "0",
  "results": [
    {"title": "…", "url": "https://…", "snippet": "…"}
  ],
  "total_results": 1,
  "response_time_ms": 1234
}
```

### `POST /v1/webfetch`

别名：`POST /webfetch`

官方网页正文抓取。上游 `POST /agenttool/v1/webfetch`，由上游取页并转 Markdown，代理只校验 `http://` / `https://`，不本地代理任意 URL。

```json
{
  "url": "https://www.tencent.com/zh-cn/investors.html",
  "prompt": "总结这家公司的投资者关系页面",
  "format": "markdown",
  "timeout": 20,
  "max_length": 100000
}
```

成功响应：

```json
{
  "object": "web.fetch",
  "url": "https://…",
  "title": "…",
  "content": "…",
  "content_type": "text/markdown",
  "content_length": 1370,
  "truncated": false,
  "status_code": 200
}
```

### `POST /v1/agentic_search`

别名：`POST /agentic_search`

官方多步研究代理。上游 `POST /agenttool/v1/agentic_search`（SSE，`search_mode=2`），官方客户端将其定位为金融/投资研究，非金融主题效果不保证。上游自主调用取数/搜索工具并输出综合结论，沿用官方配额及计费规则，延迟明显高于 `/v1/search`。

```json
{"query": "研究 A 股腾讯整体情况", "stream": false}
```

- `stream=false`（默认）：聚合后返回 `{"object":"agentic_search.result","query":"…","content":"…","events":[…]}`。
- `stream=true`：转发进度事件（`iteration` / `tool_call` / `tool_result` / `synthesis_delta`）；上游 `done` 的最终全文归一为一次 `synthesis`，再发送 `data: [DONE]`。
- 单次请求上游兜底超时 5 分钟。

错误仍为 OpenAI 风格 `{"error":{"message":"…","type":"…"}}`；上游配额不足常见 `15001`。

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
| `model` | 支持 `codebuddy/<id>` / `codebuddy:<id>` 前缀；也支持按请求选区：`cn:` / `domestic:`（国内）与 `global:` / `intl:`（国际）。无前缀时：`X-Site` > Key 绑定的区域 > 进程默认 `CODEBUDDY_SITE`。空或 `default` 归一为 `auto` |
| `stream` | `false` → `application/json`；`true` → `text/event-stream` |
| `stream_options.include_usage` | 缺省按 `true` 推送空 `choices` 的 usage 收尾 chunk；显式 `false` 跳过 |
| `max_tokens` / `max_completion_tokens` | 二者取正数，后者优先；同时映射为上游同值的 `max_tokens` 和 `max_completion_tokens`，不设正数上限时均不发送 |
| `tool_choice` | 对象型会被归一为 `auto` / `none` / `required` |
| `web_search_options` | 非 null 对象（推荐 `{}`）显式启用官方搜索桥接；省略 / null 不开启，`tool_choice:"none"` 优先关闭。目前仅作为启用声明，不解释其子字段 |

上游返回 `length` / `max_tokens` 时，Chat 的 JSON 与流式收尾均返回 `finish_reason:"length"`，不会改成 `stop`；`content_filter` 同样保留。代理不在本地按字符截断或伪造 token 用量。若显式联网搜索触发多个内部模型调用，该上限随每次模型调用发送，不是对整个搜索链路实施累计 token 限额。

#### Chat 显式联网搜索

```json
{
  "model": "auto",
  "messages": [{"role":"user","content":"搜索 Go 最新版本发布说明，并附来源链接"}],
  "web_search_options": {},
  "stream": true
}
```

Chat 也接受扩展 `tools:[{"type":"web_search"}]` / `web_search_preview`，
但普通客户端自定义 function（包括名为 `web_search` 的 function）仍交由客户端执行。
代理内部执行一轮官方检索并把结果回填模型，下游接收普通 Chat answer/chunks，
无需自行执行内部搜索工具。流式首帧和保活立即发送，最终答案聚合后回放。
不声明搜索的请求保持原有行为，不调用官方搜索接口。
完整客户端配置及中转字段透传说明见 [下游搜索接入](../guides/search.md)。

请求头 `X-Site: domestic|global` 可覆盖默认区域（模型前缀优先）。网关 Key 也可绑定区域（见 `CODEBUDDY_PROXY_API_KEYS`）。同一进程可同时持有国内号和国际号，不必再为两区域各开一个进程。

固定账号 Key（`cbp_xxx:account=<id>`）例外：区域由绑定账号推导，模型前缀或请求头不能跨区覆盖。冲突返回 400，绑定账号不存在或不可用返回 503。固定账号优先于会话钉号，失败不换号；Chat、Responses、Images 使用同一约束。

`GET /v1/models` 只返回**当前请求区域**的无前缀模型 ID（`X-Site` > Key 绑定 > `CODEBUDDY_SITE`），不再注入 `cn:` / `global:` 别名。聊天请求仍可手写带前缀的模型名。

JSON 请求体上限 **64MiB**（`httputil.MaxJSONBodyBytes`）。超过时返回 `413`：

```json
{"error":{"message":"Request body exceeds 64MB. 1M-context requests need a larger JSON body.","type":"invalid_request_error"}}
```

非法 JSON 仍为 `400` `Invalid JSON body`。出站前会把**连续的空正文** `assistant.tool_calls` 合成一条（flash 系上游拒收分散形状，11148）；已交错的 A/T 与带正文的 assistant 不合并。

**流式行为**：

1. 连接建立后**立即**发送 SSE 头与首个 `role=assistant` chunk，客户端不会把上游 TTFB 误判为挂起
2. 期间按 `CODEBUDDY_PROXY_STREAM_KEEPALIVE_MS`（默认 5000ms）发送 `: keep-alive` 注释
3. 工具调用分片使用 `delta.tool_calls[].index`（含 `0`），满足 OpenAI Java SDK 等强制校验客户端
4. 结束时发送 finish chunk（`stop` 或 `tool_calls`）
5. 默认再发一条 `stream_options.include_usage` 风格的 usage chunk（`choices: []`）；若请求显式传入 `"stream_options":{"include_usage":false}` 则跳过
6. 最后 `data: [DONE]`

经反向代理/隧道（请求带 `X-Forwarded-*` / `Forwarded` / `X-Real-IP`）时，三条流式入口均返回 `Connection: close` 并关闭该连接，避免隧道丢 FIN 后客户端复用死连接；直连请求仍为 `Connection: keep-alive`。

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
| GET | `/direct-admin/api/concurrency` | 按账号并发观测完整快照：`globalInFlight`、各账号 `inFlight` / `rpm1` / `rpm5` / `rpm60` / `peakInFlight`（RPM 为最近 1/5/60 分钟请求数除以窗口分钟数，JSON 数字可带小数；秒级分桶，启动不足窗口也按完整窗口计算）、最近一小时限流事件（含触发瞬间在途数）。仅内存计数，进程重启后清空 |
| GET | `/direct-admin/api/status` | 运行状态 + 账号摘要 + 配置快照；含 `version`、`build`（`version` / 可选 `commit` / `builtAt`）、进程级 `stats`、`concurrency` 数字汇总（`globalInFlight` / `accounts` / `rateLimits`，**均为数字**）。`?fresh=1` 时强制探活上游，并对当前号池已启用账号并行拉取最新 Credits（写入额度快照，响应带 `accountUsages`）。15 秒轮询不要带 `fresh` |
| GET | `/direct-admin/api/usage` | 用量汇总 + 分页明细 + 趋势 `series`；默认落盘 `proxy-usage.json`（约 400 条环形缓冲 + 90 日汇总） |
| GET | `/direct-admin/api/client-config` | 前端配置（baseUrl / apiKey / site / requireApiKey） |
| POST | `/direct-admin/api/client-config/generate-key` | 生成 `cbp_...` Key，写入 `~/.codebuddy/proxy.env`（或已有 `.env`）并立即生效 |
| POST · PUT | `/direct-admin/api/pool-site` | 切换**默认**号池区域 `domestic` / `global`，回写 `.env`。请求仍可用 Key 绑定、`X-Site` 或 `cn:`/`global:` 前缀覆盖 |
| POST · PUT | `/direct-admin/api/pool-product` | 切换上游产品 `codebuddy` / `workbuddy`，回写 `.env`；国内国际账号共用同一选择 |
| GET | `/direct-admin/api/checkin-config` | 返回每账号自动签到配置：`auto`（总开关）、`rules[]`、`path`（规则文件路径） |
| POST | `/direct-admin/api/checkin-config` | 保存 `{"auto":bool,"rules":[{"accountId","enabled","start","end"}]}`；写 `proxy-checkin.json` 并把总开关回写 `.env`，立即生效。非法时间/重复账号返回 `400` |

状态中的 `accounts.activeEnabledCount` 仅表示当前区域已启用数量；
`activeAvailableCount` 再排除无凭据、整号冷却及有效配额阻塞，不保证指定模型可用。
各账号的 `modelCooldowns` 按请求上游的模型 ID 返回恢复时间戳（毫秒），模型限制单独展示。

`generate-key` 会同步置 `CODEBUDDY_PROXY_REQUIRE_API_KEY=true`。**主 Key（`CODEBUDDY_PROXY_API_KEY`）立即失效**，客户端必须同步更换。`CODEBUDDY_PROXY_API_KEYS` 里的绑定 Key 不受影响。

### 账号池

| Method | Path | 说明 |
|--------|------|------|
| GET | `/direct-admin/api/codebuddy/status` | 账号池摘要 |
| GET | `/direct-admin/api/codebuddy/accounts` | 账号列表 |
| POST | `/direct-admin/api/codebuddy/accounts/import` | 批量导入账号。body `{"text":"<JSON 文本>","site":"domestic\|global","label":"可选前缀"}`；自动识别 CodeBuddy 客户端凭据 / 账号池文件 / `account+auth` 包装，按凭据去重并返回逐条结果 |
| DELETE | `/direct-admin/api/codebuddy/accounts/{id}` | 删除账号 |
| POST | `/direct-admin/api/codebuddy/accounts/{id}/enable` | 启用 |
| POST | `/direct-admin/api/codebuddy/accounts/{id}/disable` | 禁用 |
| GET | `/direct-admin/api/codebuddy/accounts/{id}/usage` | 拉取该账号 Credits（剩余/总额） |
| POST | `/direct-admin/api/codebuddy/accounts/{id}/refresh-token` | 强制刷新 token |
| POST | `/direct-admin/api/codebuddy/accounts/{id}/test` | 钉死该账号发最小 chat，验证可用性与延迟；body 可选 `{"model":"..."}`，省略时取缓存目录最低倍率模型 |
| POST | `/direct-admin/api/codebuddy/checkin` | 对指定号池内已启用账号批量每日签到；body 可选 `{"site":"domestic"}`，省略时跟随当前激活号池（`CODEBUDDY_SITE`） |
| POST | `/direct-admin/api/codebuddy/test` | 对指定号池内已启用账号批量 chat 测试（串行 + 小间隔）；body 可选 `{"site":"...","model":"..."}`，路径与 `checkin` 同级以免被 `accounts/{id}` 吞掉 |

`accounts/{id}/test` 与 `test` 均为只读探测：不写 cooldown / 选号 / session pin，失败也不会换号，也不会在测试前刷新过期 token。401 时先点「刷新 Token」再测，避免把过期登录态误判成账号不可用。最小 chat 带一条非空 system 再发 `user: ping`，避免国际站因「首条不是 system」打回 11128 而把健康号判失败。上游仍走 protocol_direct 流式聚合；管理台拿到的是 JSON。单账号超时 20s；批次总上限 5 分钟（约 `20s × 账号数`），间隔默认 350ms。模型目录缓存为空且未传 `model` 时整批失败，先点「拉取模型」。

`test` 批量响应示例：

```json
{
  "ok": true,
  "poolSite": "domestic",
  "model": "hy4-preview",
  "summary": {
    "total": 2,
    "passed": 2
  },
  "results": [
    {
      "ok": true,
      "accountId": "…",
      "site": "domestic",
      "model": "hy4-preview",
      "latencyMs": 812,
      "message": "chat ok"
    }
  ]
}
```

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

套餐明细位于单账号 usage 的 `credits.packages[]`；`cycleEndTime` 保留上游原文，新增 `expiresAt` 为绝对时间的 Unix 毫秒（未知时省略），无时区字符串按 UTC+08:00 解析。`quota.checkedAt` 是本次快照时间。管理台明细按到期时间排序并展示近期到期额度；接口查询有效套餐，不保证完整的过期历史包。

绑定 Key 管理：`POST /direct-admin/api/client-config/bound-keys` 接受 `{"site":"domestic"}` 或 `{"accountId":"..."}`；后者从账号推导区域，同时给出 `site` 时必须一致。配置回显的绑定项包含 `accountId`。账号必须存在、启用且有凭据，创建失败不会落盘。

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
