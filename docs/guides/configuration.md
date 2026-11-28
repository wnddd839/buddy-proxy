# 配置参考

配置优先级：**系统环境变量 > `.env` 文件 > 内置默认值**。
`.env` 不覆盖已存在的系统环境变量。

## `.env` 查找顺序

未设置 `CODEBUDDY_PROXY_ENV_FILE` 时，程序按以下顺序定位：

1. 当前工作目录 `./.env`
2. 上级目录 `../.env`
3. 可执行文件同目录 `.env`
4. 都没有 → 使用 `./.env`（新建）

二进制用户把 `.env` 放在 exe 旁边即可，首次启动会自动生成 API Key 并写入。

---

## 服务

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_PROXY_HOST` | `127.0.0.1` | 监听地址 |
| `CODEBUDDY_PROXY_PORT` | `32126` | 监听端口 |
| `CODEBUDDY_PROXY_MAX_BODY_MB` | `64` | 公开推理接口 JSON 上限，单位 MiB；非正值或溢出回退默认。管理接口仍为 64 MiB，图片 multipart 限制不变 |
| `CODEBUDDY_PROXY_SERVER_IDLE_TIMEOUT_MS` | `120000` | HTTP keep-alive 空闲超时（毫秒）；非正值回退默认，不是 SSE 总时长或上游首包超时 |
| `CODEBUDDY_PROXY_HTTP_TIMEOUT_MS` | `120000` | 上游**响应头**超时（毫秒）；流式 body 无总时长上限，`http.Client.Timeout` 恒为 0 |
| `CODEBUDDY_PROXY_STALL_TIMEOUT_MS` | `600000` | 上游流「连续无字节」看门狗：首字节前阈值。收到任意字节后切到 `STALL_AFTER_FIRST_BYTE_MS`。非正值/非法回退默认 |
| `CODEBUDDY_PROXY_STALL_AFTER_FIRST_BYTE_MS` | `600000` | 首字节（含 thinking / 心跳）后的看门狗阈值。长思考静默不被误杀；不是总时长上限 |
| `CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE` | `auto` | system 提示词策略 `auto` / `keep` / `fold`，管理台 06 面板可热切换并写回本文件 |
| `CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS` | `600000` | `SIGINT`/`SIGTERM` 优雅关闭宽限期（毫秒）；非正值回退默认。不影响自更新交接的 30 秒超时，Docker/systemd 的停止超时需不小于该值 |
| `CODEBUDDY_PROXY_OAUTH_PROXY` | 空 | 国际站（`global`）OAuth 专用出口，支持 `http(s)://` / `socks5(h)://`；`auth/state`、`auth/token` 与 token refresh 都走它。国内站与普通推理不受影响，非法值记 ERROR 并回退默认客户端 |
| `CODEBUDDY_PROXY_PUBLIC_BASE_URL` | 空 | 公网访问地址，用于 OAuth 回调与客户端配置回显。会去掉结尾 `/` |

## 鉴权

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_PROXY_API_KEY` | 空 | 客户端主 Key。**留空且未配置 `CODEBUDDY_PROXY_API_KEYS` 时，首次启动自动生成 `cbp_...` 并写入 `~/.codebuddy/proxy.env`** |
| `CODEBUDDY_PROXY_API_KEYS` | 空 | 多 Key 绑定区域或账号，例如 `cbp_aaa:global,cbp_bbb:domestic,cbp_fixed:account=<id>`。未写后缀只做鉴权；主 Key 也可出现在绑定表 |
| `CODEBUDDY_PROXY_REQUIRE_API_KEY` | 跟随 API Key | `true` 时 `/v1` 强制鉴权。设了主 Key 或多 Key 就默认为 `true` |
| `CODEBUDDY_PROXY_ADMIN_PASSWORD` | 空 | 管理台密码。**留空 = 管理台免密**（本地推荐），与 API Key 鉴权相互独立 |

管理台「生成 API Key」会覆写 `proxy.env` / `.env` 中的 `CODEBUDDY_PROXY_API_KEY` 并置 `REQUIRE_API_KEY=true`，立即生效、旧**主** Key 失效。`CODEBUDDY_PROXY_API_KEYS` 里的绑定 Key 不受影响。若当前目录已有 `.env` 则仍写该文件（开发方便）；否则写入 `~/.codebuddy/proxy.env`，避免换启动目录就换 Key。

同时服务国内 + 国际时，给客户端两把 Key 即可：

```env
CODEBUDDY_PROXY_API_KEYS=cbp_intl_xxx:global,cbp_cn_xxx:domestic
```

固定账号示例：

```env
CODEBUDDY_PROXY_API_KEYS=cbp_fixed:account=<账号池中的id>
```

账号绑定优先于会话钉号，区域由账号自身推导，不受当前管理台号池区域影响。若模型区域前缀或 `X-Site` 与绑定账号冲突，返回 400；账号不存在、禁用或无凭据时返回 503，不静默回退到其他账号。上游 429 等失败也不换号，但可刷新同一账号的 OAuth 凭据。此约束对 Chat、Responses、Images、搜索与网页抓取接口均生效，包括 Responses 内置搜索触发的内部调用。

管理台“新建绑定 Key”可选择具体账号；未选择账号时保留原有区域绑定。`REQUIRE_API_KEY=false` 会跳过 API Key 鉴权及绑定，请勿用于需要账号隔离的部署。

## 账号池

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_PROXY_ACCOUNTS_PATH` | `~/.codebuddy/proxy-accounts.json` | 账号池 JSON 路径，支持 `~` 与 `~/...` 展开 |
| `CODEBUDDY_PROXY_USAGE_PATH` | 与账号池同目录的 `proxy-usage.json` | 管理台用量明细与按日汇总；未设时取 `dirname(ACCOUNTS_PATH)/proxy-usage.json` |
| `CODEBUDDY_PROXY_ENV_FILE` | 自动探测 | 强制指定 env 文件路径。未设时：已有 `.env` 则用它，否则 `~/.codebuddy/proxy.env` |
| `CODEBUDDY_PROXY_QUOTA_FLOOR_CREDITS` | `0` | 选号门槛：缓存余额低于该值的账号不参与选号，`0` = 不启用。数据源与配额耗尽同一口径（缓存的 billing credits）。管理台「选号门槛」可热更新，无需重启 |

账号文件以 `0600` 权限写入。备份与迁移只需复制这个 JSON。

建议一进程一份 `proxy-accounts.json`。请求回写（Flush）会按 id 吸收磁盘上新增的账号，避免整表覆盖抹掉外部加号；Delete / Upsert 仍直接写盘。双实例同时删/加仍可能打架。无凭据账号仍会被 Normalize 丢掉。加号优先走管理台。

## 站点 / 区域

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_SITE` | `global` | 默认号池区域。`domestic` / `cn` / `china` / `internal` → 国内；其余（含空）→ 国际。单请求优先级：模型前缀 `cn:` / `global:` > 头 `X-Site` > Key 绑定 > 本值。不必为两区域开双进程 |
| `CODEBUDDY_PRODUCT` | `codebuddy` | `workbuddy` / `wb` / `ide` → WorkBuddy IDE 头与域名；其余 → CodeBuddy CLI。国内国际共用，管理台可一键切换 |
| `CODEBUDDY_INTERNET_ENVIRONMENT` | 空 | `internal` / `ioa` → `copilot.tencent.com`；`domestic` / `cn` / `china` → `www.codebuddy.cn` |
| `CODEBUDDY_BASE_URL` | 按 site + product 推导 | 显式覆盖上游基址。切产品时管理台会改写为对应门户 |

`CODEBUDDY_BASE_URL` 推导逻辑：

| 条件 | 结果 |
|------|------|
| `PRODUCT=workbuddy` 且国内 | `https://www.workbuddy.cn` |
| `PRODUCT=workbuddy` 且国际 | `https://www.workbuddy.ai` |
| `INTERNET_ENVIRONMENT` 为 `internal` / `ioa` | `https://copilot.tencent.com` |
| `SITE` 或 `INTERNET_ENVIRONMENT` 为 `domestic` / `cn` / `china` | `https://www.codebuddy.cn` |
| 其他 | `https://www.codebuddy.ai` |

> **区域**以账号自身的 `site` 为准，不看反代机器 IP。进程级 `CODEBUDDY_SITE` 只是**默认**选号区域；同一进程的号池可以同时放国内号和国际号。**产品**（CodeBuddy / WorkBuddy）是进程级选择，国内国际账号共用同一套头和域名。切号池不会改产品，切产品也不会改号池。

典型组合：

```env
# 国内
CODEBUDDY_SITE=domestic
CODEBUDDY_INTERNET_ENVIRONMENT=internal

# 国际
CODEBUDDY_SITE=global
CODEBUDDY_INTERNET_ENVIRONMENT=public
```

## 上游协议

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_CHAT_COMPLETIONS_PATH` | `/v2/chat/completions` | 上游聊天路径 |
| `CODEBUDDY_API_ENDPOINT` | 空 | 覆盖上游 API endpoint。仅账号自身值为空时参与兜底 |
| `CODEBUDDY_IDE_VERSION` | `2.117.2` | 上报给上游的 IDE 版本 |
| `CODEBUDDY_REFRESH_WINDOW_MS` | `600000` | token 提前刷新窗口（10 分钟） |
| `CODEBUDDY_BILLING_BASE_URL` | 按 site 推导 | 覆盖 Credits 查询基址 |

## 模型与流式

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_PROXY_MODELS` | `auto` | 回退模型列表，逗号分隔。留空或非法则回退 `auto` |
| `CODEBUDDY_PROXY_STREAM_KEEPALIVE_MS` | `5000` | SSE keep-alive 间隔 |

---

## 波动监测与告警

默认关闭。完整原理与告警节奏见 [波动监测与告警](../operations/monitoring.md)。

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `CODEBUDDY_PROXY_WATCH` | 关闭 | `on` / `true` 开启进程内监测 |
| `CODEBUDDY_PROXY_WATCH_INTERVAL` | `60s` | 评估周期，支持 `90s` / `2m`，纯数字按秒 |
| `CODEBUDDY_PROXY_WATCH_WINDOW` | `120` | 近期窗口样本数 |
| `CODEBUDDY_PROXY_WATCH_BASELINE` | `480` | 基线窗口样本数 |
| `CODEBUDDY_PROXY_WATCH_MIN_SAMPLES` | `20` | 低于该样本数不做判定（冷启动期） |
| `CODEBUDDY_PROXY_WATCH_ERROR_RATE` | `0.25` | 近期错误率上限；写 `25` 视为 25% |
| `CODEBUDDY_PROXY_WATCH_REASONING_MISS` | `0.5` | 请求了推理但无推理 token 的占比上限 |
| `CODEBUDDY_PROXY_WATCH_LATENCY_FACTOR` | `3.0` | 近期耗时中位数相对基线的倍数上限 |
| `CODEBUDDY_PROXY_WATCH_REMINDER` | `6h` | 持续异常时的提醒间隔 |
| `CODEBUDDY_PROXY_WATCH_WEBHOOK` | 空 | 告警 webhook，POST JSON；留空则不发 |
| `CODEBUDDY_PROXY_WATCH_WEBHOOK_TOKEN` | 空 | 以 `Authorization: Bearer` 发送 |
| `CODEBUDDY_PROXY_WATCH_SMTP_ADDR` | 空 | 邮件通道，`host:port` |
| `CODEBUDDY_PROXY_WATCH_SMTP_USER` | 空 | SMTP 用户名 |
| `CODEBUDDY_PROXY_WATCH_SMTP_PASS` | 空 | SMTP 密码 / 授权码 |
| `CODEBUDDY_PROXY_WATCH_SMTP_FROM` | 空 | 发件地址，需与认证账号一致 |
| `CODEBUDDY_PROXY_WATCH_SMTP_TO` | 空 | 收件地址，逗号分隔 |
| `CODEBUDDY_PROXY_WATCH_SMTP_MODE` | 按端口推断 | `implicit`（465）/ `starttls`（587）/ `none` |

---

## 每账号自动签到

默认关闭。管理台「自动签到」章节可配置，保存后立即生效、无需重启；也可直接写 `.env` 与规则文件。时间统一按**北京时间（UTC+8）**，不看进程本地时区。

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `CODEBUDDY_PROXY_CHECKIN_AUTO` | 关闭 | `true` / `on` 开启每账号定时签到 |
| `CODEBUDDY_PROXY_CHECKIN_ALLOW_DIRECT_FALLBACK` | 关闭 | `true` 时账号绑定的出口连不上，直连重试一次。仅对「连不上」类错误生效；429 / 额度 / 上游 5xx 不降级 |
| `CODEBUDDY_PROXY_CHECKIN_RETRY_INTERVAL_MS` | `600000` | 签到失败后多久重试一次（毫秒），`0` = 不重试。重试状态从签到日志推导，不额外存状态文件，进程重启后仍继续 |
| `CODEBUDDY_PROXY_CHECKIN_RETRY_MAX` | `1` | 当天最多重试几次（不含首次尝试） |

规则保存在账号池同目录的 `proxy-checkin.json`（原子写、`0600`），**不写进 `proxy-accounts.json`**：账号池文件会被外部编辑并被内存副本回写，混放容易互相覆盖。

```json
{
  "version": 1,
  "rules": [
    {"accountId": "账号ID", "enabled": true, "start": "09:00", "end": "11:00"},
    {"accountId": "另一个ID", "enabled": true, "start": "23:30", "end": ""}
  ]
}
```

- `end` 留空或等于 `start` = **固定时刻**；填更晚时间 = 每天在该时段内确定性地均匀随机一个秒级时刻（同一天跨重启稳定）。
- 每个账号到自己时刻时**单独签到**，不是整批跑；只对已启用且有凭据的账号生效。
- 不支持跨零点窗口（`end` 必须 ≥ `start`）；账号 ID 不可重复；时间格式 `HH:MM`。
- **启动不补签**：进程启动前错过的窗口等明天，不追补。想补签到「账号池」手动点「每日签到」。
- 上游 `todayCheckedIn` / `code=10001` 提供幂等，重复触发不会重复领奖。

---

## 固定值（不可通过环境变量配置）

| 常量 | 值 | 说明 |
|------|-----|------|
| `Transport` | `protocol_direct` | 唯一传输方式，无其他实现 |
| `HTTPTimeout` | `120s` | 上游请求超时 |
| `IdleConnTimeout` | `90s` | 空闲连接超时 |
| `MaxIdleConns` | `100` | 连接池总上限 |
| `MaxIdleConnsPerHost` | `20` | 单主机连接上限 |
| `OAuthSessionTTL` | `15m` | OAuth 会话有效期 |
| 模型缓存 TTL | `60s` | `/v1/models` 缓存时长 |
| 账号池刷盘延迟 | `250ms` | dirty 后合并异步落盘 |
| graceful shutdown | `10s` | `SIGINT` / `SIGTERM` 退出等待 |

---

## 旧版 Node 环境变量兼容

Go 版会一并读取以下旧名（新名优先）：

| 新名 | 旧名 |
|------|------|
| `CODEBUDDY_PROXY_HOST` | `CURSOR_DIRECT_HOST` |
| `CODEBUDDY_PROXY_PORT` | `CURSOR_DIRECT_PORT` |
| `CODEBUDDY_PROXY_API_KEY` | `CURSOR_DIRECT_API_KEY` · `CURSOR_GATEWAY_API_KEY` |
| `CODEBUDDY_PROXY_ADMIN_PASSWORD` | `CURSOR_DIRECT_ADMIN_PASSWORD` · `CURSOR_GATEWAY_ADMIN_PASSWORD` |
| `CODEBUDDY_PROXY_REQUIRE_API_KEY` | `CURSOR_DIRECT_REQUIRE_API_KEY` |
| `CODEBUDDY_PROXY_PUBLIC_BASE_URL` | `CURSOR_DIRECT_PUBLIC_BASE_URL` |
| `CODEBUDDY_PROXY_ACCOUNTS_PATH` | `CURSOR_DIRECT_CODEBUDDY_ACCOUNTS_PATH` |
| `CODEBUDDY_PROXY_MODELS` | `CURSOR_DIRECT_CODEBUDDY_MODELS` |
| `CODEBUDDY_PROXY_STREAM_KEEPALIVE_MS` | `CURSOR_DIRECT_STREAM_KEEPALIVE_MS` |
| `CODEBUDDY_SITE` | `CURSOR_DIRECT_CODEBUDDY_SITE` |
| `CODEBUDDY_INTERNET_ENVIRONMENT` | `CURSOR_DIRECT_CODEBUDDY_INTERNET_ENVIRONMENT` |
| `CODEBUDDY_BASE_URL` | `CURSOR_DIRECT_CODEBUDDY_BASE_URL` |
| `CODEBUDDY_API_ENDPOINT` | `CURSOR_DIRECT_CODEBUDDY_API_ENDPOINT` |
| `CODEBUDDY_CHAT_COMPLETIONS_PATH` | `CURSOR_DIRECT_CODEBUDDY_CHAT_COMPLETIONS_PATH` |
| `CODEBUDDY_REFRESH_WINDOW_MS` | `CURSOR_DIRECT_CODEBUDDY_REFRESH_WINDOW_MS` |

`CODEBUDDY_PROXY_ENV_FILE`、`CODEBUDDY_PROXY_USAGE_PATH`、`CODEBUDDY_IDE_VERSION`、`CODEBUDDY_BILLING_BASE_URL`、`CODEBUDDY_PRODUCT` 无旧名对应。

---

## 最小可用配置

```env
CODEBUDDY_PROXY_HOST=127.0.0.1
CODEBUDDY_PROXY_PORT=32126
CODEBUDDY_PROXY_REQUIRE_API_KEY=true
CODEBUDDY_PROXY_API_KEY=
CODEBUDDY_PROXY_ADMIN_PASSWORD=
CODEBUDDY_SITE=domestic
CODEBUDDY_INTERNET_ENVIRONMENT=internal
```

`API_KEY` 留空即可——首次启动会自动生成并回填。


## v0.7.5：路由、签到分布与失败转储

- 路由规则 `mode` 缺省为 `always`，保持旧行为；`onExhausted` 先用源站点原模型，只有无候选或额度类失败且同区无法换号时才推进备用链。管理台新建跨区规则默认 `onExhausted`；显式指定区域和固定账号不套用规则。
- 签到 `allDay` 使用 24 小时权重分布（夜间低权重但非零），`window` 在窗口内均匀取秒，`human` 保留独立活跃时段，`fixed` 保持显式时刻。整批错开一次性写成固定时刻，不是每天随机。仍然启动不补签。
- `CODEBUDDY_PROXY_DUMP_FAILURES` 默认空（关闭），可填写本机专用私有目录；配置后重启生效。上游 HTTP 错误可落完整请求体与诊断，最多留 6 组、单份请求约 64 MiB，文件名以 `buddy-failure-` 开头。新目录尝试 `0700`、文件 `0600`；Windows 必须另行限制 ACL，已有目录权限由运维负责。请求包含完整对话与工具参数，可能含密码/密钥/个人数据，**不是脱敏日志**；短期开启，用后关闭、重启并清理，勿放公开目录、勿上传 GitHub。不要多个实例共用转储目录。
## system 提示词处理策略

`CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE` 决定网关把客户端的 `system` 消息怎么转发给上游。
管理台「06 System Prompt」面板可热切换，无需重启。

| 值 | 行为 | 何时用 |
|---|---|---|
| `auto`（默认） | **首试前**删掉已知指纹再直传；仍被 11128 拒收时自动改写重放一次 | 日常 |
| `keep` | 只直传，被拦截如实报错 | 严格照 system 执行、或想看真实失败原因 |
| `fold` | 每次都把 system 改写进首条 user 的 `<client_instructions>` | 批量/自动化、不在意 system 优先级 |
| `strip` | 与 `auto` 相同的剥离规则与折叠兜底（保留兼容：`auto` 现在也做首试前剥离） | 兼容已有配置 |

### 剥离开关（管理台 / `.env`）

剥离不是硬编码在 `auto` 里的，它有两个独立的开关，都能热切换：

| 开关 | `.env` 键 | 默认 | 关掉会怎样 |
|---|---|---|---|
| 首试前剥离已知指纹 | `CODEBUDDY_PROXY_SYSTEM_STRIP_OFF` | 开（不配置即开） | 退回 v0.7.6 行为：先原样直传，被 11128 拒了才折叠重放 ⇒ **每个请求多一次 400**，账号风险分持续上涨 |
| 连 assistant 正文一起剥 | `CODEBUDDY_PROXY_SYSTEM_STRIP_ASSISTANT_OFF` | 开（不配置即开） | 只剥 `system`；历史里带指纹的会话会**永久 502**（折叠只改 system，救不了 assistant） |

两个键都是**跳过式**（`OFF` 语义）：不配置 = 开启，写 `1`/`true` 才关闭。这样任何
不经过 `config.Load()` 的构造路径都默认受保护，不会因为忘了设字段就静默失去剥离。

> `keep` / `fold` 永远不剥离，不受这两个开关影响 —— 它们分别是「严格照 system
> 执行」和「每次折叠」档，语义上不该被第三方改写。

### 网关删什么（auto / strip）

上游拦两类指纹，判定都在**归一化**之后（删掉 16 种空格类字符并转小写）：

1. **开头身份句**（只在消息开头命中）：
   - `You are Claude Code, Anthropic's official CLI for Claude.` —— 国内区与海外区都拦
   - `You are an AI agent powered by DeepSeek Harness.` —— 仅国内区拦（海外区放行）
2. **任意位置段落指纹**（子串匹配，仅国内区）：
   - dsh（DeepSeek Harness）自带 file-reference 段的开头
     `Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root`

判定口径是「以该句**开头**」/「任意位置包含该子串」。因此大小写、连续空格、
NBSP/全角空格/零宽空格/BOM 等变体都会命中；而身份句前加任意字符、夹在文本中间、
或句内出现 TAB/换行则不会命中。

实测**不是**关键词过滤：`You are Cline, ...`、`You are Cursor, ...`、`You are Codex, ...`、
`You are GitHub Copilot, ...`、`You are DeepSeek, an AI assistant.`、
`You are an AI agent powered by X Harness.` 全部放行。

### 剥离的边界

- 扫 `system`（含被归一成 system 的 `developer`）与 `assistant` 正文；不动 `user`/`tool`
- 上游确实会扫 `assistant`：历史里出现上述指纹时，整段会话会持续 400/11128，
  折叠 system 也救不回来，所以剥离必须覆盖它
- 任意一条 system 命中都会处理，不限首条
- 支持字符串与结构化数组（数组只动 `text` part，图片等不动）
- 只删除命中的那段文字，不引入替换文本
- 剥空的 system **保留为空消息**，不会丢弃 —— 避免海外区补 canonical 的兜底注入
- 与 `fold` 不同：剥离不改变 system 的 role、位置和其余内容
- `keep` / `fold` 不做剥离，保持与历史版本一致的形态
- 关掉「连 assistant 一起剥」后，`assistant` 正文完全不碰（见上表）

> `auto` 档先剥再传，是为了避免每个请求都先吃一次 400/11128 ——
> 上游按被拒收的次数累计账号风险分，「失败再重试」不能作为日常路径。

### 会话亲和与剥离的关系

会话键（`sessionpin.Key`）在**入口处**用原始 `system + 首条 user` 计算，早于
provider 层的剥离。因此剥离不会让同一会话的账号钉漂移。
若在剥离后再算会话键，不同客户端剥空后会撞成同一个键 —— 这是必须避免的。
