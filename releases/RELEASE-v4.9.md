## v4.9 · 2026-09-14 · 控制台模型目录

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

- **`GET /v1/models`** 改走控制台对话目录（`/console/enterprises/personal/models`），和国际站多 host 合并；失败再回落 `/v3/config`。
- 管理台小字标明本代理是 **Chat Completions** 协议（不是 Responses API）。

### 感谢

- [@240xu](https://github.com/240xu) · [#12](https://github.com/wnddd839/buddy-proxy/pull/12)
- [@zeonseoi](https://github.com/zeonseoi) · [#13](https://github.com/wnddd839/buddy-proxy/issues/13)

### 预告

**v5.0** 将支持 OpenAI Responses 协议（下游 `/v1/responses` 进出，上游仍走 Chat）。

### 升级

覆盖旧二进制后重启。配置、账号池与 Key 不用改。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
