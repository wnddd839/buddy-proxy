# v0.7.3 · 重连修复、OAuth 出口与并发观测

本版发布此前已进入主线但未随 v0.7.2 发布的可靠性改动，并补齐本轮审查发现的重连与 RPM 问题。

## 修复与改进

- 国际站客户端首条消息缺少 system 时自动补全；国内站既有请求体不变（#62）。
- 首内容前传输失败且没有可用备选账号时，同号重连并复用有界重放预算；备选账号对当前模型仍在冷却时不再误判为可用。已交付内容不会重放（#65）。
- Chat、Responses、Agentic Search 的 SSE 在请求带转发头时关闭连接复用，直连仍保持 keep-alive（#51）。
- 模型目录支持 console 失败 10 分钟负缓存、同路径多 host 与 CLI 补充目录并发；fresh=1 绕过负缓存。公开目录缓存 TTL 仍为 60 秒，IDE 元数据补全仍在前序目录完成后执行（#63）。
- 新增国际站 OAuth 专用出口，覆盖 Start、Poll、自动及手动 token refresh；国内站与普通推理调用不变（#64）。
- SIGINT/SIGTERM 优雅关闭默认宽限 10 分钟，可配置；Compose 停止等待同步为 11 分钟，自更新交接仍为 30 秒。已知 invoke model error 支持换号重试，不泛化到所有 500（#60）。
- 新增按账号在途请求、限流现场及 RPM 观测，覆盖 chat/image/search；完整快照在 `/direct-admin/api/concurrency`，管理台状态只带数字汇总。RPM 为最近 1/5/60 分钟请求数除以对应分钟数，允许小数，按秒分桶，启动不足窗口仍按完整窗口计算（#57、#54）。仅内存记录，不落盘、不额外探测。

保留 v0.7.2 的每账号自动签到、无终止帧截断处理、上游停滞检测与更新下载重试。

## 升级注意

- 原生程序可在管理台检查更新并重启，或下载对应平台二进制手动替换。保留原 `.env`、账号池、`proxies.json`、`proxy-checkin.json` 及用量文件；附件 `default.env.example` 仅供参考，不要覆盖已有配置。
- 可选 `CODEBUDDY_PROXY_OAUTH_PROXY`（例如 `socks5h://127.0.0.1:17998`）配置后需要重启。非法值会记录错误并回退默认客户端，不代表专用出口成功启用。
- `CODEBUDDY_PROXY_SHUTDOWN_GRACE_MS` 默认 `600000`；Docker `stop_grace_period` / systemd `TimeoutStopSec` 必须不小于所配宽限。现有部署需同步停止超时，单纯替换二进制不会修改部署文件。
- 隧道需向应用传递 `X-Forwarded-For`、`X-Forwarded-Host`、`Forwarded` 或 `X-Real-IP` 才启用 SSE 关闭连接策略；这不等于已对所有隧道部署完成实测。
- Docker 用户使用 `ghcr.io/wnddd839/buddy-proxy:v0.7.3`，镜像由标签工作流构建发布。

## 验证与附件

- 全量 `go test ./...`、`go test -race ./...`、`go vet ./...`、格式与构建检查通过。
- 同号重连新增回归已先复现后修复；RPM 回归覆盖历史窗口、过期样本及小数速率。
- 提供 Windows x64、Linux amd64/arm64/armv7/armv6、macOS arm64/amd64 七个平台二进制及 `SHA256SUMS.txt`。

## 致谢

特别感谢 [@240xu](https://github.com/240xu) 提交以下 PR 和 Issue，提供问题报告、复现证据、实测数据与修复方案：

- [PR #51](https://github.com/wnddd839/buddy-proxy/pull/51)：隧道 SSE 连接复用问题及部署级对照验证。
- [PR #57](https://github.com/wnddd839/buddy-proxy/pull/57) / [Issue #54](https://github.com/wnddd839/buddy-proxy/issues/54)：按账号并发观测方案、限流实测与策略建议。
- [PR #60](https://github.com/wnddd839/buddy-proxy/pull/60)：流式可靠性问题，以及本版补齐的优雅关闭宽限与瞬时 500 重试方案。
- [PR #62](https://github.com/wnddd839/buddy-proxy/pull/62)：国际站首条 system 消息补全。
- [PR #63](https://github.com/wnddd839/buddy-proxy/pull/63)：模型目录提速方案与性能实测。
- [PR #64](https://github.com/wnddd839/buddy-proxy/pull/64)：国际站 OAuth 专用出口方案。
- [PR #65](https://github.com/wnddd839/buddy-proxy/pull/65)：无备选账号时同号重连方案与传输失败实测。

这些贡献促成了本版改进。最终代码由主线自主实现、未直接合并原 PR，不影响上述贡献归属与感谢。
