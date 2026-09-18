## v0.4.9.5 · 2026-09-18 · 钉号禁删自愈 · 6004 对齐 reset

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

- 禁用或删除被钉账号时，同一会话会重选其他号，不再 502（[#23](https://github.com/wnddd839/buddy-proxy/issues/23)）。空池仍报错，`cause=pool_state`。
- 6004 冷却跟 `will reset at`（UTC+8 / RFC3339，含 ChatError `[region=…]` 信封），上限 48 小时；解析失败仍 2 分钟。看日志 `cooldown_source=error_text` 还是 `fixed_2m`（[#22](https://github.com/wnddd839/buddy-proxy/issues/22)）。
- Flush 按 id 吸收磁盘新增，避免整表回写抹掉外部加号（[#25](https://github.com/wnddd839/buddy-proxy/issues/25)）。Delete 不会把已删号救回来。双实例同时改仍可能打架；继续一进程一文件。
- 管理台 02 页只显示当前号池；03 页能看见、复制、删除、新建绑区 Key（[#24](https://github.com/wnddd839/buddy-proxy/issues/24)）。不删主 Key。

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) · [#23](https://github.com/wnddd839/buddy-proxy/issues/23) · [#22](https://github.com/wnddd839/buddy-proxy/issues/22) · [#25](https://github.com/wnddd839/buddy-proxy/issues/25) · [#24](https://github.com/wnddd839/buddy-proxy/issues/24)

### 升级

覆盖旧二进制后重启。账号池不用迁移。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
