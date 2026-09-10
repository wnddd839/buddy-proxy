## v4.3 · 2026-09-10 · DeepSeek Flash 1M 上下文不再卡在 70%

### 下载哪个文件？

| 你的系统 | 下载 |
|----------|------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| Linux x64 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

> Windows 请勿下载 `darwin-*` 或 `linux-amd64`。

校验：`SHA256SUMS.txt`

---

### 解决了什么

ZCode 用 DeepSeek V4 Flash（1M 窗口）时，上下文大约到 **70%** 就报超出长度；同样 1M 的 GLM / HY4 没问题。别的 CodeBuddy 反代能跑满 1M。

这不是上游把窗口砍掉了。CLI 目录把 Flash 标成「思考关不掉」，ZCode 按约 30% 预留思考预算，于是 `1M × 0.7` 就炸。另外，以前 JSON 体只读 8MiB，接近 1M 的中文 + 工具请求可能被截成非法 JSON。

### 改了什么

- 拉模型时再打一枪 IDE `/v3/config`，把 `canDisableThinking` / `supportedEfforts` 补到 CLI 列表上。
- CLI 独有的 `deepseek-v4-flash` 也会标成可关闭思考。
- 聊天 JSON 上限 **8MiB → 64MiB**；超限返回明确的 **413**。

v4.2 已经透传了真实的输入/输出上限。本版补上思考开关元数据，Flash 不应再被锁死在 70%。

### 升级

直接用新二进制覆盖旧的即可，`.env` 与账号池不用改。

重启后让 ZCode **重新拉模型列表**（或 `GET /v1/models?fresh=1`）。若客户端仍开着 high 思考，输出/思考还是会占窗口，这是正常的。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
