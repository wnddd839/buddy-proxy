## v0.5.2 · 2026-09-22 · 账号 Chat 测试

### 下载哪个文件？

| 你的系统 | 下载 |
|----------|------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| Linux x64 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

校验：`SHA256SUMS.txt`

---

### 改了什么

- 管理台可对单个账号或当前号池批量发最小 chat，看是否可用、延迟多少（[#29](https://github.com/wnddd839/buddy-proxy/issues/29)）。
- 测试钉死目标账号，失败不换号，也不写入冷却。默认用最低倍率模型。
- 过期 token 不会在测试时自动刷新；看到 401 先点「刷新 Token」。
- 顶栏版本旁多一段 UI 修订号 `2026.09.22-chat-test`，用来确认管理台页面已更新。

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) · [#29](https://github.com/wnddd839/buddy-proxy/issues/29)

### 升级

覆盖旧二进制后重启。账号池不用迁移。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
