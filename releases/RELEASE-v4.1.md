## v4.1 · 2026-09-10 · 管理台每日签到

### 下载哪个文件？

| 你的系统 | 下载 |
|----------|------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| Linux x64 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

> Windows 请勿下载 `darwin-*` 或 `linux-amd64`。兼容旧名 `windows-amd64.exe`。

校验：`SHA256SUMS.txt`

---

### 做了什么

- 管理台首页增加「每日签到」，一键为当前号池全部已启用账号领取每日积分。
- 国内站约 100 积分/天（连续第 7 天 1000）；国际站没有活动时会提示并跳过。
- 单账号 20 秒、整批最多 5 分钟；超时未签的账号记为跳过。
- 新接口：`POST /direct-admin/api/codebuddy/checkin`。

### 怎么用

1. 打开管理台「概览与监控」
2. 切到要签的号池（国内 / 国际）
3. 点「每日签到」

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
