## v0.5.5 · 2026-10-04 · 上游图片生成 / 编辑

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

- 新增 `POST /v1/images/generations` 与 `POST /v1/images/edits`，转上游 `/v2/images/*`。目录里 `tags` 含 `text-to-image` 的模型标 `mode=image_generation`，不要打 chat。
- 图片选号对齐 chat：鉴权失败 refresh 同号一次，429/502/503/504 换号；不钉会话。
- 账号 Chat 测试跳过出图模型，不再按最低倍率误选到它们。

### 升级

覆盖旧二进制后重启。账号池不用迁移。出图模型走 Images API，不要打 `/v1/chat/completions`。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
