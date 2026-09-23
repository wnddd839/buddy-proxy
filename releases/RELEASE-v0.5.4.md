## v0.5.4 · 2026-09-23 · 国际站账号测试不再误报 11128 · 断电不再把账号文件写成全 0

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

- 管理台国际站「测试 / 批量测试」不再因为只发 `user: ping` 误报 11128（[#31](https://github.com/wnddd839/buddy-proxy/pull/31)）。国内站行为不变。
- 账号池、用量、`.env` 写入在 rename 前 fsync，避免断电后留下长度正确、内容全 0 的文件（[#32](https://github.com/wnddd839/buddy-proxy/pull/32)）。

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) · [#31](https://github.com/wnddd839/buddy-proxy/pull/31) · [#32](https://github.com/wnddd839/buddy-proxy/pull/32)

### 升级

覆盖旧二进制后重启。账号池不用迁移。已经是全 0 的损坏文件救不回来，需要从备份或重新登录恢复。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
