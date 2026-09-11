## v4.5 · 2026-09-11 · 管理台可切换 CodeBuddy / WorkBuddy 上游

### 下载哪个文件？

| 你的系统 | 下载 |
|----------|------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| Linux x64 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

> Windows 请勿下载 `darwin-*` 或 `linux-amd64`。

校验：`SHA256SUMS.txt`

---

### 解决了什么

国际站一直走 CodeBuddy CLI 头。WorkBuddy 没有 CLI，要用 IDE 头才能打到 WorkBuddy 目录。现在国内 / 国际账号都可以在管理台一键切换上游，token 共用、不用重新登录。

### 改了什么

- 号池旁增加 **CodeBuddy / WorkBuddy** 开关，和国内 / 国际一样切换。
- WorkBuddy：国际 `www.workbuddy.ai`，国内 `www.workbuddy.cn`，请求头为 VSCode + WorkBuddy UA。
- 默认仍是 CodeBuddy CLI。选择写入 `.env` 的 `CODEBUDDY_PRODUCT`。

### 升级

覆盖旧二进制后重启。要走 WorkBuddy 请切管理台开关，或设 `CODEBUDDY_PRODUCT=workbuddy`，然后刷新模型列表。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
