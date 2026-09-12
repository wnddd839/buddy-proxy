## v4.6 · 2026-09-12 · 同会话钉号，换号按额度拿最大

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

同一段对话以前可能轮询到不同账号，prompt cache 对不齐。现在同会话钉在一个号上；要换号时现场查剩余额度，拿最大的那个。

### 改了什么

- 同一会话钉在第一次成功的账号；429 / 额度耗尽才松钉换号。
- 冷号池 / 缺快照 / 快照超过 5 分钟：新会话才探活额度；之后按快照选剩余最大者。
- 换号前并行刷新候选额度（最多 2.5s），再选最厚的号。
- WorkBuddy 缓存命中按实测字段解析，并补齐出站 `cache_read_input_tokens`。

### 升级

覆盖旧二进制后重启。配置与账号池不用改。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
