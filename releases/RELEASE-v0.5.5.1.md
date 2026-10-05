## v0.5.5.1 · 2026-10-05 · 目录补缺不再顶掉 console 的 credits

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

- 模型目录补上 `glm-5.0-turbo` / `minimax-m2.7` / `hunyuan-image-alpha-edit`：console 目录成功后，再用同一套 CLI 身份头拉 `/v3/config` 补缺（失败按最佳努力忽略）。
- 修目录合并：CLI 的 `auto` 行不再顶掉 console 的 `default` 行（两者对外都是 `auto`）。之前这一覆盖会把 `credits` / `credit_multiplier` 抹空，账号 Chat 测试的选号也会漏掉它。
- 补缺只加 console 里没有的新 id，console 已有行元数据（`credits` / `name`）原样保留。
- `docs/api/http.md` 同步规则说明。

### 感谢

- [@240xu](https://github.com/240xu) · [#35](https://github.com/wnddd839/buddy-proxy/issues/35) · 6 身份 × 6 端点 × 3 host 探测矩阵

### 升级

覆盖旧二进制后重启。账号池不用迁移。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
