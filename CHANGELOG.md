# 更新日记

> CodeBuddy Proxy（Go · `protocol_direct`）发布说明。  
> 仓库：[`wnddd839/buddy-proxy`](https://github.com/wnddd839/buddy-proxy)

---

## 未发布

---

## v0.5.3 · 2026-09-23 · flash 11148 合并分散 tool_calls · 顶栏不再逐字折行

### 感谢

- [@carter003](https://github.com/carter003) 在 [#30](https://github.com/wnddd839/buddy-proxy/issues/30) 用对照矩阵证伪：flash 系拒收连续分散的空 `assistant.tool_calls` + 后置集中 `tool` 结果（11148），合并成一条即过。
- [@kouekikin24](https://github.com/kouekikin24) 在 [#33](https://github.com/wnddd839/buddy-proxy/issues/33) 指出切号池后探测文案变长，顶栏每个标签内部逐字折行。

### 解决了什么

1. `deepseek-v4.1-flash` / `deepseek-v4-flash` 在「两条空 assistant 各带 1 个 tool_call + 后置集中 tool 结果」时稳定 11148；同请求换 hy4/auto/glm 则过。v0.5.1 的类型归一挡不住这个形状。
2. 切国内/国际号池后几秒，探测文案变长，顶栏 5 个标签各自内部折行，中文逐字断开。

### 改了什么

- `EnsureUpstreamMessages` 合并**连续的空正文** `assistant.tool_calls`（T2/T7 → T1）。已交错的 A/T（T4）和带正文的 assistant 不合并。孤儿 `tool`（T5）仍透传。
- 管理台顶栏 `.pill` / `.pillrow` 禁止内部折行、禁止被压窄；过宽时整条状态条换到品牌下一行。UI 修订号改为 `2026.09.23-status-pills`。

### 升级注意

覆盖旧二进制后重启。账号池不用迁移。管理台顶栏版本旁应看到 `ui 2026.09.23-status-pills`。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.5.3

---

## v0.5.2 · 2026-09-22 · 账号 Chat 测试

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) 在 [#29](https://github.com/wnddd839/buddy-proxy/issues/29) 建议管理台对齐 New API 渠道测试：钉死账号发一条最小 chat，看可用性与延迟。

### 解决了什么

管理台账号池只能启用、禁用、查用量、刷新令牌、删除，没有办法单独验证某个账号能不能聊天。

### 改了什么

- 单账号 `POST /direct-admin/api/codebuddy/accounts/{id}/test`，批量 `POST /direct-admin/api/codebuddy/test`（与 checkin 同级，避免被 `accounts/{id}` 吞掉）。
- 钉死目标账号后直接 `Provider.Complete`，不走换号，不写 cooldown / 选号。默认模型取缓存目录最低倍率，可在 body 里覆盖 `model`。
- 批量串行，间隔 350ms；单账号 20s，批次上限 5 分钟。只读探测不会先刷新过期 token，401 时先点「刷新 Token」。
- 错误摘要按 rune 截断，中文不再在 UTF-8 边界被切成乱码。
- 管理台账号行「测试」、顶部「批量测试」和共享模型下拉。顶栏版本旁显示 UI 修订号 `2026.09.22-chat-test`。

### 升级注意

覆盖旧二进制后重启。账号池不用迁移。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.5.2

---

## v0.5.1 · 2026-09-21 · Responses 工具回传配对 11148

### 感谢

- [@carter003](https://github.com/carter003) 在 [#28](https://github.com/wnddd839/buddy-proxy/issues/28) 指出 Responses 回传工具结果时 `tool_calls` 的 Go 切片类型对不上过滤逻辑，上游打回 11148。

### 解决了什么

Codex 等客户端走 `POST /v1/responses`，第二轮把 `function_call` + `function_call_output` 再塞进 `input`。翻译层把 `tool_calls` 写成 `[]map[string]any`，上游过滤只认 `[]any`，assistant 被当空消息丢掉，只剩 `tool` 结果，上游回 `11148 tool calls and tool results do not match`。

### 改了什么

- `EnsureUpstreamMessages` 把 `tool_calls` 的 `[]map[string]any` 与 `[]any` 归一后再判断是否为空消息。Responses 第二轮的 `assistant.tool_calls` 不再被丢掉，工具结果能和调用配对。
- 日志指纹同样走归一函数。附 `ToMessages` → `EnsureUpstreamMessages` 回归测试。

### 升级注意

覆盖旧二进制后重启。账号池不用迁移。v0.5 的 Codex 配置不用改。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.5.1

---

## v0.5 · 2026-09-20 · Responses API · 同会话复用上游会话 ID

### 感谢

- [@carter003](https://github.com/carter003) 在 [#27](https://github.com/wnddd839/buddy-proxy/pull/27) 提出同会话复用上游 `X-Conversation-ID`，并用直连对照实验给出了缓存波动数据。

### 解决了什么

1. Codex CLI 等只认 OpenAI Responses API 的客户端无法直连：旧版 `POST /v1/responses` 鉴权后直接 400。
2. 同会话每次请求都换新的上游 `X-Conversation-ID`，prompt cache 命中不稳（PR #27 在 `hy4-preview` 上观察到波动）。

### 改了什么

- **Responses API**：实现 `POST /v1/responses`（别名 `/responses`）。入口把 `input` / `instructions` / 扁平 `tools` 译成 Chat Completions，打同一套号池与会话钉；非流式回完整 `response` 对象，流式回命名 SSE（`response.created` → `*.delta` → `response.completed`）。`background` 明确 400；内置工具与图像输入丢弃；无服务端 `store`。Codex 配置见 README。
- **同会话复用 `X-Conversation-ID`**：新增 `sessionpin.ConversationTable`（与账号钉同生命周期，45 分钟空闲过期；换号/站点/产品即轮换）。`request/message ID` 仍逐请求随机；无会话键的请求行为不变。
- **管理台刷新状态拉最新额度**：点「刷新状态」会 `GET /direct-admin/api/status?fresh=1`，对当前号池已启用账号并行打官网套餐接口并回写快照。15 秒自动轮询仍只读缓存，不打 billing。
- 管理台接入页增加 Responses URL 复制；架构/HTTP 文档同步。

### 升级注意

覆盖旧二进制后重启。账号池不用迁移。Codex CLI 把 `wire_api` 设为 `responses`，Base URL 填 `http://127.0.0.1:32126/v1`。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.5

---

## v0.4.9.6 · 2026-09-18 · 6004 解析生产文案尾巴

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) 在 [#26](https://github.com/wnddd839/buddy-proxy/pull/26) 用生产实收报文证伪：v0.4.9.5 仍会把 `will reset at … UTC+8, alternatively…` 整段送进解析器，冷却静默回落 2 分钟。

### 解决了什么

v0.4.9.5 已按 `will reset at` 对齐 6004 冷却，也切掉了 ChatError 的 `[region=…]` 信封。真实上游正文在时间戳后紧跟英文逗号和 `alternatively…`，逗号不在旧截断集里，解析失败后又打回固定 2 分钟——#22 的客户循环还在。

### 改了什么

- 旧 `extractWillResetAt` 保持不变。解析失败时按时间戳形状兜底（`YYYY-MM-DD[ T]HH:MM:SS` + 可选 `UTC+8` / RFC3339 偏移）。
- 仍走 `ParseBillingTimeMillis`；`UTC+8` 剥掉后按 Asia/Shanghai。非 +8（`UTC-8` / `UTC+9`）不硬换算，回落 2 分钟。
- 48 小时上限、没有合法时间仍 2 分钟、6004 不进额度探测：都不动。看日志 `cooldown_source=error_text` 还是 `fixed_2m`。

### 升级注意

覆盖旧二进制后重启。账号池不用迁移。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.4.9.6

---

## v0.4.9.5 · 2026-09-18 · 钉号禁删自愈 · 6004 对齐 reset · Flush 合号 · 管理台按池

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) 提出 [#23](https://github.com/wnddd839/buddy-proxy/issues/23)：禁号/删号后被钉会话硬失败 502；[#22](https://github.com/wnddd839/buddy-proxy/issues/22)：6004 冷却固定 2 分钟、不读上游恢复时间；[#25](https://github.com/wnddd839/buddy-proxy/issues/25)：外部加号被整表回写抹掉；[#24](https://github.com/wnddd839/buddy-proxy/issues/24)：管理台 02 页不按当前号池过滤、03 页看不见绑区 Key。

### 解决了什么

1. 长会话钉在某账号上时，管理台禁用或删除该号，同一会话会立刻 502，且绕过换号，直到 pin TTL。
2. 上游 6004（模型额度/频控）文案里带了 `will reset at`，号池却一律冷却 2 分钟，到期后又打到同一张还在封顶的号。
3. 运行中往 `proxy-accounts.json` 加号，下一次 Flush 会把内存两份整表写回去，金丝雀账号消失。
4. 管理台 02 页混着两个区域的号；03 页只展示主 Key，`CODEBUDDY_PROXY_API_KEYS` 绑区 Key 看不见也管不了。

### 改了什么

- **钉号自愈（[#23](https://github.com/wnddd839/buddy-proxy/issues/23)）**：禁用、删除、无凭据、site mismatch 都会 Forget 钉号并重选。池里还有别的号时，客户端拿到 200。空池仍报错，`cause=pool_state`（不是 `upstream_infra`）。
- **6004 冷却（[#22](https://github.com/wnddd839/buddy-proxy/issues/22)）**：解析错误文案里的 `will reset at`（无时区按 Asia/Shanghai，也认 RFC3339；生产 ChatError 的 `[region=…]` 信封也会先截掉）。写入 `cooldownUntil`，上限 48 小时。解析失败仍 2 分钟。看日志 `cooldown_source=error_text` 还是 `fixed_2m`。6004 没有进额度探测。
- **Flush 按 id 吸收磁盘新增（[#25](https://github.com/wnddd839/buddy-proxy/issues/25)）**：避免整表回写抹掉外部加号。同一 id 以内存热字段为准；Delete 仍直接写盘，不会把已删 id 救回来。双实例同时删/加仍可能打架。继续一进程一文件。
- **管理台（[#24](https://github.com/wnddd839/buddy-proxy/issues/24)）**：02 页只列出当前号池账号（另一区有号时空态提示去切区域，不说「请先 OAuth」）。03 页列出绑区 Key：预览、区域、复制、删除；可按 domestic/global 新建绑定 Key，写入 `CODEBUDDY_PROXY_API_KEYS` 并热更新。不删主 Key。

### 升级注意

覆盖旧二进制后重启。账号池不用迁移。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.4.9.5

---

## v0.4.9.4 · 2026-09-17 · API Key 绑区域 · 目录不再灌别名

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) 提出 [#20](https://github.com/wnddd839/buddy-proxy/issues/20)：`cn:` / `global:` 别名把同一模型拆成 2–3 份，ZCode 下拉框从 22 项涨到 73 项。

### 解决了什么

v0.4.9.3 用模型名前缀表达区域，`GET /v1/models` 在两区都有号时额外返回 `cn:` / `global:` 副本。按模型名组织 UI 的客户端会把同一模型显示多次。多数客户端也没有自定义 `X-Site` 的入口。

### 改了什么

- **Key → 区域**：`CODEBUDDY_PROXY_API_KEYS=cbp_aaa:global,cbp_bbb:domestic`。一把 Key 绑定一个号池区域；换 Key 等于换区。未出现在这张表里的主 Key（`CODEBUDDY_PROXY_API_KEY`）行为不变。
- **干净目录**：`GET /v1/models` 只返回**当前请求区域**的无前缀模型 ID，不再注入 `cn:` / `global:` 别名。区域来源：`X-Site` > Key 绑定 > 进程默认 `CODEBUDDY_SITE`。
- **聊天选区**：`模型前缀 > X-Site > Key 绑定 > CODEBUDDY_SITE`。`cn:` / `global:` 前缀仍可打聊天，只是不再出现在模型列表里。
- 已配置 `CODEBUDDY_PROXY_API_KEYS` 时，启动不再额外生成一把主 Key。

### 升级注意

覆盖旧二进制后重启。若客户端曾把 `cn:deepseek-…` 写进模型列表，改回纯名字，并用两把 Key（或 `X-Site`）区分区域。单 Key 用户看到的目录会回到默认区域的一份干净列表。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.4.9.4

---

## v0.4.9.3 · 2026-09-16 · 按请求选区 · 上游就绪探测 · Markdown 标题

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) 提出 [#17](https://github.com/wnddd839/buddy-proxy/issues/17)：一个进程同时服务国内 + 国际号池；[#18](https://github.com/wnddd839/buddy-proxy/issues/18)：`/health` 上游挂了仍绿灯。
- [@zeonseoi](https://github.com/zeonseoi) 提出 [#19](https://github.com/wnddd839/buddy-proxy/issues/19)：流式 Markdown 标题缺空格被当纯文本。

### 解决了什么

1. 同时持有国内号和国际号时，以前只能开两个进程、两份 `.env`。国际站 502 时国服其实是活的，单实例却用不上。
2. `/health` 只表示进程存活。上游 chat 后端 502/504 时监控仍绿灯。
3. GLM 等模型常输出 `##标题`（ATX 缺空格），再叠上 SSE 把 `\n` 和 `##` 拆成两片，客户端就把标题当纯文本。

### 改了什么

- **按请求选区**：`CODEBUDDY_SITE` 只是默认区域。模型前缀 `cn:` / `global:`（以及 `domestic:` / `intl:`）或请求头 `X-Site` 决定本轮号池；账号对象里本来就有 `site`。两区都有号时，`GET /v1/models` 额外返回带前缀别名。钉号 key 带 site，`cn:` 与 `global:` 并行互不抢 pin。
- **就绪探测**：`GET /readyz` 与 `GET /health?deep=1` 向 chat 端点发空 POST（无 token）：401 = 鉴权层可达，502/504 + HTML = 上游基础设施故障。结果按 `site|product` 缓存 20s，切 SITE / 产品立刻清空。`/readyz` 的 `ok` 是 anyOK；`upstream.probed` 在填完 Sites 后为 `true`。
- **错误归因**：上游 5xx 标 `cause: upstream_infra`；401 标 `account_blocked`；400 不再当成基础设施。有 `Retry-After` 时用 `errors.AsType` 从包装错误里取出并回写响应头。
- **Markdown ATX**：流式 `text_delta` 不再把单独的 `\n` Trim 掉；行首 `##`–`######` 后面若缺空格则补上。单个 `#`（`#include` / shebang / `#define`）不动。

### 升级注意

覆盖旧二进制后重启。账号池与 Key 不用改。不必再为两区域开第二个进程。监控请改看 `/readyz` 或 `/health?deep=1`，不要把 `/health` 当成上游可用性。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.4.9.3

---

## v0.4.9.2 · 2026-09-16 · 钉号切站自愈 · 流式 tool_calls index

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) 提出 [#15](https://github.com/wnddd839/buddy-proxy/issues/15)：SITE 切换后旧会话钉号硬失败。
- [@kkl31415926](https://github.com/kkl31415926) 提出 [#16](https://github.com/wnddd839/buddy-proxy/issues/16)：流式 `tool_calls[].index` 缺失导致 Android Studio / OpenAI Java SDK 报错。

### 解决了什么

1. 长会话钉到某区域账号后，管理台切换 SITE，同一会话会立刻 `site mismatch`，且绕过换号自愈，直到 pin TTL。
2. 流式工具调用分片里 `index=0` 被 `omitempty` 丢掉，严格客户端解析失败；普通对话不受影响。

### 改了什么

- **钉号 × SITE**：`Select` 遇 `site mismatch` 时 Forget 钉号、清 AccountID 并重选当前区域账号。
- **流式 tool_calls**：拆出 `ToolCallDelta`，`index` 必填（含 `0`）；非流式 `message.tool_calls` 仍不含 index。
- **`stream_options.include_usage`**：缺省仍推送 usage 收尾 chunk；显式 `false` 时跳过。

### 升级注意

覆盖旧二进制后重启。账号池与 Key 不用改。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.4.9.2

---

## v0.4.9.1 · 2026-09-14 · 下游 system 折叠

### 感谢

- [@itaid](https://github.com/itaid) 提出 [#14](https://github.com/wnddd839/buddy-proxy/issues/14)：Claude Code 的 system 指纹触发上游 11128。
- [@zeonseoi](https://github.com/zeonseoi) 提出 [#13](https://github.com/wnddd839/buddy-proxy/issues/13)：Qoder CN 同类封装。

### 解决了什么

ZCode、Claude Code、Qoder 都是客户端把身份写进 `system`，上游当非法渠道打回 11128。以前按品牌逐句替换，每来一家改一次。

### 改了什么

- 客户端 `system` / `developer` 收成一条短 system，原文折进第一条 user；用户与工具结果原样透传。
- 不再为 ZCode / Claude Code 单独改字符串。Responses 协议仍未做。

### 升级注意

覆盖旧二进制后重启。客户端不用改配置。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v0.4.9.1

---

## v4.9 · 2026-09-14 · 控制台模型目录

### 感谢

- [@240xu](https://github.com/240xu) 提出 [#12](https://github.com/wnddd839/buddy-proxy/pull/12)：`GET /v1/models` 走 IDE 的 `/v3/config` 会列出点了就 11102 的模型，并漏掉 hy4-preview 等可对话模型。问题测对了；本版按仓库规范重写后收进发布（未直接合并该 PR）。
- [@zeonseoi](https://github.com/zeonseoi) 提出 [#13](https://github.com/wnddd839/buddy-proxy/issues/13)：Qoder CN 接入。本版管理台标明本代理是 Chat Completions 协议。

### 解决了什么

模型列表以前用 IDE 插件目录，和真正能打 `/v2/chat/completions` 的对不上：多出 codewise 一类、少了 hy4-preview / glm 等，也没有 `credits`。

### 改了什么

- **`GET /v1/models`**：优先拉 `/console/enterprises/personal/models`（CLI 头，国际站多 host 合并），CodeBuddy 产品仍用 IDE `/v3/config` 补 `reasoning`；失败回落 `/v3/config`。WorkBuddy 号不会把 token 打到 `copilot.tencent.com`。多 host 部分失败时，`Message` 带上非致命错误。
- 管理台接入页小字标明本代理是 **Chat Completions** 协议；`POST /v1/responses` 本版仍未实现，有鉴权的请求返回 400。

### 预告

**v5.0** 将支持 OpenAI Responses 协议：下游请求 `/v1/responses` 按 Responses 进出，请求 Chat 仍走 Chat；上游还是 CodeBuddy chat。本版先标明协议，不做适配。

### 升级注意

覆盖旧二进制后重启。账号池与 Key 不用改。客户端继续填 Chat Completions Base URL。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.9

---

## v4.8 · 2026-09-13 · API Key 固定路径 · 用量按账号/模型

### 感谢

- [@carter003](https://github.com/carter003) 提出 [#10](https://github.com/wnddd839/buddy-proxy/issues/10)：统计表要能看到账号，并按账号 / 模型筛选。
- [@carter003](https://github.com/carter003) 提出 [#11](https://github.com/wnddd839/buddy-proxy/issues/11)：hy3 缓存命中率几乎为 0，要分清是模型还是统计。

### 解决了什么

换文件夹启动、从压缩包双击、或进程里有空的 `CODEBUDDY_PROXY_API_KEY=` 时，每次都会生成新网关 Key，客户端仍拿旧 Key 就 401。用量页只有总命中率，hy3 和 DeepSeek 混在一起看不清。

### 改了什么

- **API Key 落盘**：无现成 `.env` 时写入 `~/.codebuddy/proxy.env`（与账号池同目录）。加载时空环境变量不再挡住文件里的 Key。当前目录已有 `.env` 时仍优先用它（开发方便）。
- **用量筛选**：Tab 05 可按账号、模型过滤明细与汇总；账号列优先显示用户名（有自定义标签则为 `标签 · 用户名`）。
- **按模型命中率**：同一时间窗按模型列出请求数 / Token / 缓存命中率，用来对比 hy3 与 DeepSeek。公式仍是「缓存 token ÷ prompt token」，代理不会替上游「补」不存在的 cache 字段。
- 不加 SQLite：明细仍约 400 条 JSON + 90 日按日汇总。

### 升级注意

覆盖旧二进制后重启。账号池与 `proxy-usage.json` 不用改。若客户端已配过 Key：把原来的 `CODEBUDDY_PROXY_API_KEY=cbp_...` 写进 `%USERPROFILE%\.codebuddy\proxy.env`（或启动目录已有的 `.env`），即可沿用旧 Key。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.8

---

## v4.7 · 2026-09-12 · 管理台用量明细与版本号

### 感谢

- 社区 [#9](https://github.com/wnddd839/buddy-proxy/issues/9)：管理台展示版本号、请求明细与 token / credit 统计。

### 改了什么

- **版本号**：顶栏与 `/health`、`/direct-admin/api/status` 展示构建版本（`make build VERSION=v4.7` 注入）。
- **Tab 05 · 用量与明细**：今日 / 7 日 / 30 日（历史不足 30 天时与 7 日同窗口）；表格含代理 request id、上游 `X-Conversation-Request-ID`、会话摘要、token / cache、上游 `usage.credit`、耗时与账号。
- **用量持久化**：`proxy-usage.json`（与账号池同目录，可 `CODEBUDDY_PROXY_USAGE_PATH`）；环形明细约 400 条 + 90 日按日汇总，重启保留。
- **Admin API**：`GET /direct-admin/api/usage`（分页、趋势 `series`、缓存命中率）。
- **上游追踪**：`RequestTrace` 从 protocol_direct 请求头回填；`ParseUsage` 解析单次 `credit`。

### 说明

- 客户端取消连接仍记为成功行（与原有 `stats` 一致），失败计数只含上游/网关错误。
- 本地 `go build` 未带 `VERSION=` 时显示 `dev`。
- **Breaking（仅 API）**：`GET /direct-admin/api/usage?range=process` 已移除；旧客户端若仍传 `process` 会按 `day` 处理（数据已落盘，不再提供「进程内全量」语义）。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.7

---

## v4.6 · 2026-09-12 · 同会话钉号，换号按额度拿最大

### 感谢

本版由社区 issue 推动。感谢：

- [@carter003](https://github.com/carter003) 提出 [#8](https://github.com/wnddd839/buddy-proxy/issues/8)：同一会话应钉在一个账号上，新会话再按剩余额度选号，才能吃到 prompt cache。
- [@tearslee](https://github.com/tearslee) 提出 [#7](https://github.com/wnddd839/buddy-proxy/issues/7)：经代理走 DSH/Codex 时缓存读取一直是 0。

### 解决了什么

多账号号池以前按轮询换号。同一段对话的工具轮、续写会打到不同账号，上游 prompt cache 对不齐，客户端看到的 `cache_read_input_tokens` 也经常是 0。WorkBuddy 真实命中写在 `prompt_tokens_details.cached_tokens` / `prompt_cache_hit_tokens` 上，顶层 `cache_read_input_tokens` 和 `cached_tokens` 即使命中也是 0。

### 改了什么

- **会话粘滞**：同一会话钉在第一次成功的账号；该号 429 / 额度耗尽时松钉再换。
- **新会话选号**：用缓存的剩余额度选最大者。缺失快照或超过 5 分钟的号才会并行探活（冷号池、中途新登录、只查过部分 Credits 都覆盖）；成功请求不再清掉这笔缓存。同会话续写不打 billing。
- **换号前探活**：对未冷却候选并行刷新额度（只打套餐接口，整批最多 2.5s，失败保留缓存），再拿剩余最大的号。
- **WorkBuddy usage**：按实测字段解析缓存命中，出站补齐 `prompt_tokens_details.cached_tokens`、`prompt_cache_hit_tokens`、`cache_read_input_tokens`。

### 升级注意

用新二进制覆盖后重启即可。配置与账号池格式不变。客户端无需改请求头；有 `X-Session-Id` / `prompt_cache_key` 时优先用，否则用 system + 首条 user 的哈希。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.6

---

## v4.5 · 2026-09-11 · 管理台可切换 CodeBuddy / WorkBuddy 上游

### 解决了什么

国际站聊天一直走 CodeBuddy CLI 头和 `www.codebuddy.ai`。WorkBuddy 没有 CLI 产品，真正的 IDE 指纹是 `User-Agent: VSCode/… WorkBuddy/…`，目录也不同（国际含 `hy4-preview` / `deepseek-v4.1-flash` / `gpt-6-astra`）。国内国际账号 token 共用，但之前没法在反代里一键切到 WorkBuddy。

### 改了什么

- 管理台号池旁增加 **CodeBuddy / WorkBuddy** 分段开关，交互与国内 / 国际号池相同。
- 进程级 `CODEBUDDY_PRODUCT`（默认 `codebuddy`），国内国际账号共用；切号池不会改产品。
- WorkBuddy：国际打 `www.workbuddy.ai`，国内打 `www.workbuddy.cn`，请求头为 VSCode + WorkBuddy UA。
- CodeBuddy：保持原来的 CLI 头与原域名。
- OAuth 仍走 CodeBuddy 门户，不用重新登录。
- Admin API：`POST /direct-admin/api/pool-product`，body `{"product":"workbuddy"}`。

### 升级注意

用新二进制覆盖后重启。默认仍是 CodeBuddy；要切 WorkBuddy 请打开管理台开关，或在 `.env` 里设 `CODEBUDDY_PRODUCT=workbuddy`。切完请刷新模型列表。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.5

---

## v4.4 · 2026-09-11 · 换号重试不再提前放弃剩余账号

### 感谢

本版修复由社区 issue 推动。感谢：

- [@carter003](https://github.com/carter003) 提出 [#6](https://github.com/wnddd839/buddy-proxy/issues/6)：换号重试把「累计次数」和「剩余账号数」直接比较，导致还有账号没试就提前终止。
- [@dyed-fanxing](https://github.com/dyed-fanxing) 提出 [#4](https://github.com/wnddd839/buddy-proxy/issues/4)：DeepSeek V4 Flash 在 ZCode 里 1M 上下文大约卡在 70%（已于 **v4.3** 修复）。

### 解决了什么

多账号号池里，第一个号返回 429 / 503 等可恢复错误后，可能直接报：

```text
CodeBuddy 请求重试深度超限（max=1）
```

下一个号根本没被请求。原始 429 / 503 也会被这条超限错误盖掉，排障更难。

### 根因

`RetryDepth` 是已经尝试过的次数，旧的 `completeRetryLimit(ExcludeIDs)` 却按**还没排除的剩余账号数**动态缩小。失败一次后两者反向变化，比较会提前成立。两个可用账号时：试完 A 再递归，限额变成 1，而深度已经是 1，B 就被跳过。

### 改了什么

- 已尝试账号只记在 `ExcludeIDs` 里，选号时跳过。
- `RetryDepth` 只跟固定上限 **16** 比较，防止无限递归。
- 号池试尽或撞到上限时，把最后一次真实上游错误返回给客户端，不再用「重试深度超限」覆盖 429 / 503。
- 补测试：A=429、B=成功 → 继续打 B 并成功；两个号都 429 → 返回原始 429。

### 升级注意

用新二进制覆盖旧文件后重启即可。配置与账号池格式不变。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.4

---

## v4.3 · 2026-09-10 · DeepSeek Flash 1M 上下文不再卡在 70%

### 解决了什么

ZCode 使用 DeepSeek V4 Flash（上游声明 1M 输入）时，上下文大约到 **70%** 就报超出长度；同样 1M 窗口的 GLM / HY4 正常。别的 CodeBuddy 反代能跑满 1M。这不是上游把窗口砍掉了，是我们发给客户端的**模型元数据**和请求体上限有问题。

### 根因

1. **思考关不掉，客户端预留约 30%。** 我们用 CLI 头拉 `/v3/config`。DeepSeek Flash（`deepseek-v4-flash` / `deepseek-v4.1-flash`）在 CLI 目录里是 `onlyReasoning: true` + `effort: high`，**没有** `canDisableThinking`。ZCode 把它当成思考模型且关不掉，按约 30% 预留思考预算：`1M × 0.7 ≈ 70%` 就报超窗。GLM / HY4 的 CLI 目录自带 `canDisableThinking: true`，所以不受影响。WorkBuddy / 不少其它反代用 VSCode 头，同一模型能拿到 `canDisableThinking` 和 `supportedEfforts`。
2. **JSON 体以前只读 8MiB。** 中文 + 工具定义的接近 1M 请求可能在代理侧被截断，表现成 Invalid JSON。其它 FastAPI 反代通常没有这么紧的上限。

v4.2 已经把 `max_output_tokens` / `limit.output` 透传出去（Flash 输出上限约 5 万），避免客户端把 1M 再按比例预留输出。但思考元数据缺失时，ZCode 仍会按「思考始终开启」预留 30%，所以 v4.2 之后这个问题还会在 Flash 上出现。

### 改了什么

- **补齐思考元数据：** 拉 CLI 模型列表后再用 VSCode 头打一次 IDE `/v3/config`，把 `canDisableThinking`、`supportedEfforts`、`defaultEffort` 合并进 CLI 目录（不覆盖 CLI 已有字段）。这些值会进入 `GET /v1/models` 的 `reasoning_config`、`variants.none`、`reasoning_options` 的 toggle，以及 `GET /v1/model/info` 的 `supports_none_reasoning_effort`。
- **CLI 独有的旧 Flash：** IDE 目录可能没有 `deepseek-v4-flash`。只要该模型已有 `reasoning` 对象但缺 `canDisableThinking`，代理会补 `true`（上游接受 `thinking: disabled`）。
- **请求体上限 8MiB → 64MiB。** `POST /v1/chat/completions` 超限返回 **413**，错误信息明确说明是 body 超过 64MB，而不再变成含糊的 Invalid JSON。

### 升级注意

1. 用新二进制覆盖旧文件后重启。配置与账号池格式不变。
2. 让 ZCode **重新拉模型列表**（管理台「刷新模型」，或 `GET /v1/models?fresh=1`）。代理列表默认缓存 60 秒，客户端也可能自己缓存。
3. 列表里应出现可关闭思考（`variants.none` / `reasoning_config.canDisableThinking`）。若 ZCode 仍把思考开在 high，窗口照样会被思考占用，这是正常的；只是不应再被错误地锁死在 70%。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.3

---

## v4.2 · 2026-09-10 · 透传上游上下文长度

### 模型列表

- `GET /v1/models` 透传上游 `maxInputTokens` / `maxAllowedSize` / `maxOutputTokens`，映射为 `context_length`、`max_input_tokens`、`max_output_tokens` 与 `limit.{context,output}`。
- `GET /v1/model/info` 同步带上 LiteLLM 的 `max_input_tokens`、`max_output_tokens`、`max_tokens`（输出上限）。
- 管理台模型 JSON 同样保留这三项原始字段，方便核对。

### Release 资产

- 不再发布冗余的 `codebuddy-proxy-windows-amd64.exe`：它与 `codebuddy-proxy-windows-x64.exe` 内容完全相同。Windows 64 位请下载 **`codebuddy-proxy-windows-x64.exe`**。
- 历史版本（≤ v4.1）仍保留该旧名，使用 v4.2 请改用它名字。

---

## v4.1 · 2026-09-10 · 管理台每日签到

### 管理台

- 首页增加「每日签到」：一键为**当前激活号池**里全部已启用账号领取每日积分。
- 国内站约 100 积分/天，连续第 7 天可达 1000；国际站若上游没有活动会提示并跳过。
- 单账号总计 20 秒，整批最多 5 分钟；超时还没签到的账号记为跳过，批次仍算失败。
- Admin API：`POST /direct-admin/api/codebuddy/checkin`（可传 `{"site":"domestic"}`，不传则跟随当前号池）。

下载：https://github.com/wnddd839/buddy-proxy/releases/tag/v4.1

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
