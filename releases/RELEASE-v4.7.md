## v4.7 · 2026-09-12 · 管理台用量明细与版本号

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

- 管理台顶栏 **version**，新 Tab **05 / 用量与明细**（`#usage`）。
- 记录每次 chat 的 token、cache、上游 credit（若有）、上游 request id。
- API：`GET /direct-admin/api/usage?range=day|week|month`（`range=process` 已废弃，等同 `day`）。
- 30 日统计：若 `proxy-usage.json` 里最早有数据不足 30 天，图表与汇总自动按最近 7 日展示。

### 感谢

- [@carter003](https://github.com/carter003) · [#9](https://github.com/wnddd839/buddy-proxy/issues/9)

### 升级

覆盖旧二进制后重启。配置与账号池不用改。用量默认写入与账号池同目录的 `proxy-usage.json`（可用 `CODEBUDDY_PROXY_USAGE_PATH` 改路径），正常重启后保留；`SIGKILL` 可能丢最近未落盘的几条。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
