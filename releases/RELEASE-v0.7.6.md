# Buddy Proxy v0.7.6

## v0.7.6 · 2026-11-17 · 本地扣费记账、System Prompt 四档面板与流停滞看门狗

特别感谢 [@240xu](https://github.com/240xu) 在 [#72](https://github.com/wnddd839/buddy-proxy/pull/72)、[#73](https://github.com/wnddd839/buddy-proxy/pull/73)、[#74](https://github.com/wnddd839/buddy-proxy/pull/74) 提供线上取证、控制变量探测与核心实现。三个 PR 均经过多轮根因定位、独立编码审查与补修加固后依次合并纳入；以下说明以最终发布实现为准。

### 新功能与改进

- **请求后本地实时扣减余额（#74）**：`MarkResult` 按上游实际返回的 `Usage.Credit`（`>0` 且为有限数）立即扣减账号内存 `QuotaRemaining` 并累计 `QuotaSpentSinceCheck`，无需等待最长 6 小时的轮询即可让积分下限（`CODEBUDDY_PROXY_QUOTA_FLOOR_CREDITS`）在单次会话连续请求期间即时生效。新增探测前累计扣费基线（`QuotaBaseline` / `ApplyQuotaProbe`），防止慢速 billing 回包抹平探测在途期间刚发生的本地扣费；普通上游冷却降级恢复时保留有效正余额，低于积分下限且快照超过 5 分钟 TTL 的账号纳入新会话低频自动补探。
- **System Prompt 开发者面板（#72 / #73）**：管理台新增一级导航 `06 / System Prompt`（原 `06 用量与明细` 顺延为 `07`），支持四档策略热切换并串行持久化至 `.env`：
  - `auto`（默认）：直传 `system`，命中上游 `11128` 时锁定同账号自动折叠为 `<client_instructions>` 重放一次；
  - `keep`：只直传 `system`，被拒收如实返回错误且不把账号打进 5 分钟故障冷却；
  - `fold`：始终折叠改写；
  - `strip`：按站点精确剥离上游拒收的开头身份句（Claude Code 双站拦截句、DeepSeek Harness 国内站拦截句），其余 `system` 原位保留；若遇到未收录的新 `11128` 句子同样自动折叠兜底。
- **隐私安全与全链路事件回溯**：实时环形缓冲（500 条）与按天 `proxy-systemprompt-YYYYMMDD.jsonl`（保留 7 天）仅记录原始 `system` 的 SHA-256 前 8 位指纹、字节数、剥离字节数、客户端推断（`clientHint`）与会话键来源（`sessionSource`），绝不保存 `system` 或 `user` 明文；`07 用量与明细` 新增 `System` 列，支持按 `proxyRequestId` 互查（含多轮内置搜索形态独立回填与重试失败事件完整留痕）。
- **DeepSeek Harness 会话亲和修复（#73）**：针对 `x-deepseek-harness-session-id` 为进程内自增序号（重启归零）的特性，将其与持久化 `x-deepseek-harness-user-id` 及首条 `user` 消息确定性短哈希（覆盖纯图片首轮）组合为复合会话键，消除跨实例与同机进程重启后撞上旧 `session-1` 导致的账号钉及上游 `X-Conversation-ID` 串味。
- **两档流停滞看门狗（#72）**：新增 `CODEBUDDY_PROXY_STALL_TIMEOUT_MS`（首字节前）与 `CODEBUDDY_PROXY_STALL_AFTER_FIRST_BYTE_MS`（收到任意首字节后底层 `Read` 直接切换），默认均提升至 10 分钟（`600000`），避免长思考回合静默被 3 分钟阈值误杀，同时防止毫秒配置乘法溢出意外关闭看门狗。

### 审查加固与根因修复

- 修复 `deductQuotaLocked` 原地修改共享 `*float64` 指针导致的并发 Data Race，补齐 `cloneAccount`（含 `QuotaRemaining`、`ModelCooldowns`、`AuthStatus.LoggedIn`）与 `UpdateAccount` 回调入参克隆，以及 `QuotaSpentSinceCheck` 在重启读盘、重导入、OAuth 刷新与缓存清理四条路径的一致性。
- 修复 `strip` 模式跨递归重试共享累加 `StripReport` 导致同区重试字节数翻倍与跨区 `onExhausted` 降级假阳性的问题；修复多模态结构化 `[]any` 下原地修改调用方入参 `map` 的问题；将身份句归一化改为按最长规则有界前缀扫描，消除超长或重复前缀下的平方级开销并修正句末标点前空白剥离。
- 修复嵌套 `error` JSON 丢失 `code=11128` 以及本地代理端口 `:11128` 误触发渠道拦截判定的问题；修复管理台历史事件表漏「客户端」表头导致列错位的问题。

### 审核与验证

`gofmt`、`go vet ./...`、`go test -race -count=1 ./...` 全绿，真实二进制 + Playwright 浏览器端到端回归覆盖 7 个一级章节、System Prompt 四档切换持久化、双表 11 列对齐、模型可用数、草稿恢复、XSS 转义及桌面/390px 窄屏无横向溢出。

### 升级

替换对应平台二进制或更新 Docker 镜像；不覆盖 `.env`、账号池、签到配置及历史文件。未配置新环境变量时默认 `CODEBUDDY_PROXY_SYSTEM_PROMPT_MODE=auto`、两档看门狗各 10 分钟，平滑兼容旧版数据。千问上游建议 [#21](https://github.com/wnddd839/buddy-proxy/issues/21) 仍为未实现需求，本版不包含 QwenWork 支持。

## 下载与校验

提供 Windows x64、Linux amd64/arm64/armv7/armv6、macOS arm64/amd64 七种二进制，随附 `SHA256SUMS.txt` 和 `default.env.example`。请校验 SHA-256；示例配置仅供参考，不要覆盖现有配置。Docker 镜像为 `ghcr.io/wnddd839/buddy-proxy:v0.7.6`（标签推送后由 GitHub Actions 构建发布）。
