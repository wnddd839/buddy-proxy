## v0.4.9.3 · 2026-09-16 · 按请求选区 · 上游就绪探测 · Markdown 标题

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

- 同一进程可同时服务国内 + 国际号池。模型用 `cn:` / `global:`，或请求头 `X-Site`；不必再开第二个进程（[#17](https://github.com/wnddd839/buddy-proxy/issues/17)）。
- 新增 `GET /readyz` 与 `GET /health?deep=1`：廉价探测上游 chat 端点（401=可达，502/504 HTML=基础设施故障），20s 缓存。`/health` 仍只表示进程存活（[#18](https://github.com/wnddd839/buddy-proxy/issues/18)）。
- 流式 Markdown 保留换行分片，并把行首 `##标题` 修成 `## 标题`；`#include` / shebang 不动（[#19](https://github.com/wnddd839/buddy-proxy/issues/19)）。

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) · [#17](https://github.com/wnddd839/buddy-proxy/issues/17) · [#18](https://github.com/wnddd839/buddy-proxy/issues/18)
- [@zeonseoi](https://github.com/zeonseoi) · [#19](https://github.com/wnddd839/buddy-proxy/issues/19)

### 升级

覆盖旧二进制后重启。配置不用改。监控请改看 `/readyz`。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
