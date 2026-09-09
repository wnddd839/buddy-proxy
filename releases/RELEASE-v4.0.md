## v4.0 · 2026-09-09 · 号池配额冷却与 Go 1.26 现代化

### 下载哪个文件？

| 你的系统 | 下载 |
|----------|------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| Linux x64 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

> Windows 请勿下载 `darwin-*` 或 `linux-amd64`。兼容旧名 `windows-amd64.exe`。

校验：`SHA256SUMS.txt`

---

### 号池 / 计费

- **配额感知选号**：缓存上游 billing 配额到账号；配额耗尽时在冷却窗口内跳过该账号，避免无效轮询。
- **配额错误探测**：上游返回配额类错误时主动拉取 billing usage，用 `resetAt` 对齐账号 `cooldownUntil`。
- **重试深度**：`completeRetryLimit` 随当前区域活跃账号数缩放（上限 16），多账号代理下减少过早放弃。
- **管理台**：账号卡片展示配额耗尽与恢复时间（`quotaExhausted` / `quotaResetAt`）。

### 工程

- Go 1.26 现代化：`new(expr)`、`sync.OnceValue`（billing 时区）、内建 `min`/`max`、`slices.Contains`、`t.Context()`、`QuotaState` 字段 `omitzero`。
- 删除未使用的 `gateway.Service.ApplyQuotaFromUsage` / `SyncAccountQuota`；server 层直接编排 `QuotaStateFromUsage` + `Pool.ApplyQuotaState`。
- 补 `QuotaState` JSON `omitzero` 表驱动测试，锁定 admin usage API 的 `quota` 字段序列化行为。

### 验证

- `go build ./...`、`go vet ./...`、`go test ./...` 全绿。
- review 修复：quota 文件 LF 行尾、`nul` 误建文件已删除。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
