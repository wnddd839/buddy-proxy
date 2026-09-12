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
| `CODEBUDDY_PROXY_PUBLIC_BASE_URL` | 空 | 公网访问地址，用于 OAuth 回调与客户端配置回显。会去掉结尾 `/` |

## 鉴权

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_PROXY_API_KEY` | 空 | 客户端 API Key。**留空时首次启动自动生成 `cbp_...` 并写入 `.env`** |
| `CODEBUDDY_PROXY_REQUIRE_API_KEY` | 跟随 API Key | `true` 时 `/v1` 强制鉴权。设了 API Key 就默认为 `true` |
| `CODEBUDDY_PROXY_ADMIN_PASSWORD` | 空 | 管理台密码。**留空 = 管理台免密**（本地推荐），与 API Key 鉴权相互独立 |

管理台「生成 API Key」会覆写 `.env` 中的 `CODEBUDDY_PROXY_API_KEY` 并置 `REQUIRE_API_KEY=true`，立即生效、旧 Key 失效。

## 账号池

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_PROXY_ACCOUNTS_PATH` | `~/.codebuddy/proxy-accounts.json` | 账号池 JSON 路径，支持 `~` 与 `~/...` 展开 |
| `CODEBUDDY_PROXY_USAGE_PATH` | 与账号池同目录的 `proxy-usage.json` | 管理台用量明细与按日汇总；未设时取 `dirname(ACCOUNTS_PATH)/proxy-usage.json` |
| `CODEBUDDY_PROXY_ENV_FILE` | 自动探测 | 强制指定 `.env` 写入位置 |

账号文件以 `0600` 权限写入。备份与迁移只需复制这个 JSON。

## 站点 / 区域

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CODEBUDDY_SITE` | `global` | `domestic` / `cn` / `china` / `internal` → 国内；其余（含空）→ 国际 |
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

> **区域**以账号自身的 `site` 为准，不看反代机器 IP。**产品**（CodeBuddy / WorkBuddy）是进程级选择，国内国际账号共用同一套头和域名。切号池不会改产品，切产品也不会改号池。

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
