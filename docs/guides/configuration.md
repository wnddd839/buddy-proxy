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

默认关闭。管理台「客户端接入 → 自动签到」可配置，保存后立即生效、无需重启；也可直接写 `.env` 与规则文件。时间统一按**北京时间（UTC+8）**，不看进程本地时区。

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `CODEBUDDY_PROXY_CHECKIN_AUTO` | 关闭 | `true` / `on` 开启每账号定时签到 |

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

- `end` 留空或等于 `start` = **固定时刻**；填更晚时间 = 每天在该时段内确定性地随机一个分钟（同一天跨重启稳定）。
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
