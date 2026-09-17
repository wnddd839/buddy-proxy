## v0.4.9.4 · 2026-09-17 · API Key 绑区域 · 目录不再灌别名

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

- `GET /v1/models` 只返回当前请求区域的无前缀模型 ID，不再注入 `cn:` / `global:` 别名（[#20](https://github.com/wnddd839/buddy-proxy/issues/20)）。
- 新环境变量 `CODEBUDDY_PROXY_API_KEYS=cbp_aaa:global,cbp_bbb:domestic`：一把 Key 绑定一个区域，换 Key 等于换区。
- 聊天选区：`模型前缀 > X-Site > Key 绑定 > CODEBUDDY_SITE`。`cn:` / `global:` 仍可手写，只是不再出现在下拉框。

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) · [#20](https://github.com/wnddd839/buddy-proxy/issues/20)

### 升级

覆盖旧二进制后重启。ZCode 建议配两把 Key 各绑一区，模型名改回纯名字。单 Key 用户会看到默认区域的一份干净列表。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
