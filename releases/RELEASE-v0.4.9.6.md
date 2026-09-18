## v0.4.9.6 · 2026-09-18 · 6004 解析生产文案尾巴

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

- 生产 6004 文案 `will reset at 2026-09-19 04:23:04 UTC+8, alternatively…` 也能解析，不再静默回落 2 分钟（[#26](https://github.com/wnddd839/buddy-proxy/pull/26) / [#22](https://github.com/wnddd839/buddy-proxy/issues/22)）。
- 旧截断逻辑保持权威；失败才按时间戳形状提取。`UTC+8` 仍按 Asia/Shanghai；非 +8 不硬换算。
- 48h 上限、解析失败仍 2 分钟。看日志 `cooldown_source=error_text` 还是 `fixed_2m`。

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) · [#26](https://github.com/wnddd839/buddy-proxy/pull/26) · [#22](https://github.com/wnddd839/buddy-proxy/issues/22)

### 升级

覆盖旧二进制后重启。账号池不用迁移。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
