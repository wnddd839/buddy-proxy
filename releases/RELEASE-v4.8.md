## v4.8 · 2026-09-13 · API Key 固定路径 · 用量按账号/模型

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

- 网关 API Key 默认写入 `~/.codebuddy/proxy.env`，换文件夹启动不再换一把 Key。
- 用量页可按 **账号 / 模型** 筛选；增加按模型缓存命中率表（对比 hy3 / DeepSeek）。
- 账号列优先显示用户名。不加 SQLite。

### 感谢

- [@carter003](https://github.com/carter003) · [#10](https://github.com/wnddd839/buddy-proxy/issues/10) · [#11](https://github.com/wnddd839/buddy-proxy/issues/11)

### 升级

覆盖旧二进制后重启。若要沿用客户端里已有的 Key，把它写进 `%USERPROFILE%\.codebuddy\proxy.env`。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
