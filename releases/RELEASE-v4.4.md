## v4.4 · 2026-09-11 · 换号重试不再提前放弃剩余账号

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

### 感谢

- [@carter003](https://github.com/carter003) · [#6](https://github.com/wnddd839/buddy-proxy/issues/6) 指出换号重试会提前终止
- [@dyed-fanxing](https://github.com/dyed-fanxing) · [#4](https://github.com/wnddd839/buddy-proxy/issues/4) 反馈 DeepSeek Flash 1M 上下文卡在约 70%（已于 v4.3 修复）

### 解决了什么

两个可用账号时，A 返回 429 / 503 后可能直接报 `重试深度超限（max=1）`，B 还没被请求。原始上游错误也会被这条超限信息盖掉。

### 改了什么

- 已尝试账号只走 `ExcludeIDs`，不再拿累计次数和剩余账号数直接比较。
- 单请求最多尝试 **16** 个账号。
- 号用尽时回传真实的 429 / 503，而不是「重试深度超限」。

### 升级

直接用新二进制覆盖旧的即可，`.env` 与账号池不用改。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
