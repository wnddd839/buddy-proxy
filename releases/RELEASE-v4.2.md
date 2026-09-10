## v4.2 · 2026-09-10 · 透传上游上下文长度

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

### 做了什么

把上游 `/v3/config` 的上下文与输出上限透传给下游客户端，避免客户端自己猜测窗口大小：

- `GET /v1/models` 新增 `context_length`、`max_input_tokens`、`max_output_tokens` 与 `limit.{context,output}`。
- `GET /v1/model/info` 的 LiteLLM `model_info` 同步带上 `max_input_tokens`、`max_output_tokens`、`max_tokens`。
- 管理台模型 JSON 保留 `maxInputTokens` / `maxOutputTokens` / `maxAllowedSize` 三项原始字段，方便核对。

取值规则：`maxInputTokens` 优先，缺失时回落 `maxAllowedSize`；非正数或不解析的值一律当作「没有」。

### 为什么有用

以前客户端看不到真实上限，只能靠模型名猜（例如把 1M 窗口再按比例预留一大块输出），结果长会话过早触发上游长度错误。现在可以直接按上游的实际值规划上下文与输出预算。

> 注意：这些值来自上游 `/v3/config` 声明，代理如实透传、不做修正。若个别模型的上游声明与服务端实际限制不一致（例如上游声明 1M 但服务端更早拒绝），属于上游侧问题，代理不会自行改写。

### 升级

直接用新二进制覆盖旧的即可，配置文件与账号池格式不变，无需改动 `.env`。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
