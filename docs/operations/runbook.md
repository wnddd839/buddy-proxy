# Operations Runbook

## 启动 / 停止

```bash
cd go-codebuddy
make build
./bin/codebuddy-proxy
```

信号：`SIGINT` / `SIGTERM` 触发 graceful shutdown（10s 超时），
期间会强制 flush 账号池，避免丢失内存中的统计与凭据变更。

常用命令：

```bash
make fmt         # gofmt -w
make vet         # go vet ./...
make test        # go test ./...
make test-race   # go test -race ./...
make check       # fmt + vet + test-race
make release     # 交叉编译四平台 + SHA256SUMS.txt
```

CI（`.github/workflows/ci.yml`）在 push / PR 到 `main` 时执行 `gofmt` 检查 + `go vet` + `go test -race`。

## 健康检查

```bash
curl -fsS http://127.0.0.1:32126/health
# {"ok":true,"provider":"codebuddy","transport":"protocol_direct","version":"v4.7"}
```

管理台 **05 / 用量与明细**（`#usage`）展示请求日志；默认持久化到 `CODEBUDDY_PROXY_USAGE_PATH`（缺省与账号池同目录的 `proxy-usage.json`）。`GET /direct-admin/api/usage` 见 `docs/api/http.md`。

## 日志

默认 JSON `slog` 到 stdout（**WARN** 级别；`CODEBUDDY_PROXY_LOG_LEVEL=info|debug` 可见启动细节）。关注字段：

- `codebuddy proxy starting` — 启动细节（默认 `CODEBUDDY_PROXY_LOG_LEVEL=warn` 不输出；设 `info`/`debug` 可见）
- `generated and saved gateway api key` — 首次启动自动生成 Key（**仅 debug**，不含明文；用户看管理台）
- `codebuddy oauth credential refreshed`
- `retrying codebuddy request with next account`
- `retrying codebuddy request after oauth refresh`
- `shutting down`

> 日志禁止输出完整 token，仅输出 `masked` 预览。

## 排障

| 现象 | 排查 |
|------|------|
| listen 失败 | 端口占用；换 `CODEBUDDY_PROXY_PORT` |
| 401 API | Key 不匹配 / `REQUIRE_API_KEY`；管理台重新生成 Key 后客户端需同步更换 |
| 无模型 | 未 OAuth；看 accounts path；点管理台「刷新模型」 |
| 上游 401/403 | 管理台 refresh-token，或重新 OAuth |
| 11140 / request illegal | site 与账号不匹配；检查账号 `site` 与 `CODEBUDDY_SITE` |
| 11128 / unapproved channel | 网关已映射 `role=developer`→`system`。上游非 2xx 时进程 WARN 日志 `codebuddy upstream failure` 会带脱敏 `request` 指纹（role 序列 / tools 名 / thinking / reasoning / 单消息 200 字预览，不含 token 与完整 prompt），直接看是哪字段触发。也检查 `site=domestic` 时 endpoint 是否为 `copilot.tencent.com`（错误信息含 `[region=... endpoint=...]`） |
| 国内账号却打到海外 / 反过来 | 已按**账号 site** 选端点，不看反代机器 IP，也不被进程级 `CODEBUDDY_BASE_URL` 带跑偏 |
| 11101 / tool_choice unmarshal | 客户端传了对象型 `tool_choice`；网关归一成 `auto`/`none`/`required`。Sub2API 只发 `"hi"` 探活时网关会自动补 system |
| 11102 / model service not found | 模型在列表里但上游无服务；换 `auto` 或其它可用模型 |
| Turn execution failed / reason=unknown | 看管理台 `stats.lastError`；多为上游 11128/11101 被客户端包装 |
| 跨域写管理台 API 报 403 | CSRF 保护生效；确认请求 `Origin` 与站点一致 |
| 账号统计丢失 | 进程被 `SIGKILL` 时内存中的 dirty 统计可能未落盘；正常退出会 flush |
| 每日签到失败 / 无反应 | 管理台「每日签到」作用于**当前激活号池**（跟随 `CODEBUDDY_SITE`，可切换号池后重试）；单账号总计 20s，批次上限 5 分钟（账号多时需等待）。上游 `active:false` → `supported` 缺省（不支持）；HTTP 401/单账号超时 → `failed` 且批次 `ok=false`；批次 deadline 耗尽、未执行的账号 → `skipped`（批次仍 `ok=false`）；`code=10001` → 幂等成功 |

## 账号池持久化

- 内存为权威，`Select` / `MarkResult` 只改内存并标 dirty
- **冷却**：上游失败（429/5xx/11140 等）会写入账号 `cooldownUntil`，冷却期内不参与轮询；成功请求清零
- **会话粘滞**：同一会话钉在第一次成功的账号；该号 429 / 额度耗尽时松钉再换号
- **换号重试**：429/5xx/rate limit / 额度耗尽等可恢复错误会排除当前号。换号前对未冷却候选**并行刷新额度**（2.5s 超时，失败保留缓存），再选剩余额度最大者；单请求最多尝试 16 个账号。新会话只补探 **没有快照或超过 5 分钟** 的号（中途新登录的号、只查过部分 Credits 的号也会被补上），快照新鲜则不打 billing。全部失败时把最后一次上游错误返回给客户端，不会变成「重试深度超限」
- **全冷却降级**：同区域全部冷却时仍选最快恢复账号（debug 日志），避免短时整体不可用
- 250ms 合并异步落盘，去掉每请求多次同步 IO
- OAuth / Upsert / Delete / Replace / SetEnabled **同步刷盘**（凭据不可丢）
- 原子写：temp 文件 + rename，权限 `0600`
- 备份：复制 `CODEBUDDY_PROXY_ACCOUNTS_PATH` 指向的 JSON

## 资源占用参考

本地冒烟（空闲）：

- 二进制约 **7–8MB**（`-ldflags="-s -w"`，按平台略有差异）
- 常驻内存显著低于 Node 版

## OAuth「登录入口已失效」

根因：launch/callback 在锁外读共享 `*OAuthSession`，与 `StartOAuth` 原地重置并发时 data race。

Go 版修复：

- launch/callback 用 `OAuthLaunchAuthorized(id, token)` 在锁内比对凭据
- 业务字段通过 `CurrentOAuth()` / `LiveOAuthSession()` **值快照**读取，不再外泄可变指针
- OAuth 会话 TTL 15 分钟；`reuseExisting=true` 且仍在 TTL 内会复用 `waiting` 会话

验证：重新点「开始认证」，用**新生成**的 launch 链接，应 302 到上游登录页。
