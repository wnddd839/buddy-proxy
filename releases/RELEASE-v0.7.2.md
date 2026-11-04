## v0.7.2 · 2026-11-04 · 每账号自动签到与流式收尾加固

### 新功能 · 每账号自动签到

管理台「客户端接入 → 自动签到」为每个账号单独配置固定时刻或时段随机，时间统一按**北京时间（UTC+8）**：

| 配置 | 行为 |
| :--- | :--- |
| 开始 `09:00`，结束留空或相同 | 每天北京时间 09:00 签到 |
| 开始 `09:00`，结束 `11:00` | 每天在 09:00–11:00 内确定性随机一个分钟签到（同一天跨重启稳定） |
| 该行不勾选启用 | 该账号不参与自动签到 |

- 每个账号到自己的时刻**单独签到**，不是整批跑；只对已启用且有凭据的账号生效。上游 `todayCheckedIn` / `code=10001` 提供幂等，重复触发不会重复领取。
- 总开关 `CODEBUDDY_PROXY_CHECKIN_AUTO`（默认关闭），规则保存在账号池同目录的 `proxy-checkin.json`（原子写、`0600`），**不写入 `proxy-accounts.json`**，避免账号池被外部编辑或内存回写时互相覆盖。
- 管理台保存后立即生效、无需重启；新增 `GET` / `POST /direct-admin/api/checkin-config`。非法时间、重复账号、跨零点窗口（`end < start`）在保存时拒绝。
- **启动不补签**：进程启动前已过点的窗口当天不再追补，避免开机瞬间打上游；想补签仍用「账号池 → 每日签到」手动跑一轮。原手动按钮与批量签到行为不变。
- 不配置或总开关关闭时，行为与 v0.7.1 完全一致。

```env
# 可选：每账号自动签到总开关（默认关闭），规则在管理台配置
CODEBUDDY_PROXY_CHECKIN_AUTO=true
```

完整参数见 [配置参考](https://github.com/wnddd839/buddy-proxy/blob/v0.7.2/docs/guides/configuration.md)，排障见 [运维手册](https://github.com/wnddd839/buddy-proxy/blob/v0.7.2/docs/operations/runbook.md)。

### 修复

- **回答说到一半被静默截断**：上游以 FIN 正常收尾却没给终止帧时，旧逻辑由 `FinishReasonFromTurn` 补 `finish_reason=stop`，把半截回答当完整结果。现在首内容前按传输层失败换号重放（共用 `maxResetReplays` 预算），已下发正文则显式返回 `truncated` 错误。
- **上游假活导致请求无限挂起**：新增 `provider.stallGuard`，流式响应连续 3 分钟收不到任何字节即收敛为可判定失败；按**实际下发状态**分流——内容尚未下发（含非流式已缓存部分正文）仍可换号恢复，已下发则显式报错。固定常量，不新增环境变量。
- **#61 自更新报 `unexpected EOF`**：下载 `SHA256SUMS.txt` 等 Release 文件时响应体被中途掐断，旧逻辑直接把链路瞬断显示成更新失败。现在整段重取最多 3 次，读后按 `Content-Length` 校验实际字节数；重试耗尽返回带原始原因的明确错误。取消下载、SHA-256 校验失败、非官方重定向等保护不变。

### 说明

- 流式修复吸收了 [#60](https://github.com/wnddd839/buddy-proxy/pull/60) 的两个核心思路并在本仓库简化实现（PR 未合并）：未引入 #60 的两个新环境变量、10 分钟关闭宽限与 500 换号重试，避免与 `compose.yaml` 的 `stop_grace_period: 20s` 冲突。
- 保留 v0.7.1 的模型冷却、中文重置时间解析与部署配置，以及 v0.7.0 的波动监测、批量导入和固定出口。SSE 连接复用与账号出口策略不变。

### 升级

保留现有 `.env`、账号池、`proxies.json` 和用量文件，替换对应平台二进制，或在管理台手动检查更新。七个平台二进制附 `SHA256SUMS.txt`；`default.env.example` 仅供参考，**不要覆盖已有配置**。

Docker 用户待对应标签的 Docker release 工作流成功后使用 `ghcr.io/wnddd839/buddy-proxy:v0.7.2`，或按原流程更新 `latest` 镜像。保留数据卷，不要执行 `docker compose down -v`。

### 贡献与验证

感谢 [@240xu](https://github.com/240xu) 在 [#60](https://github.com/wnddd839/buddy-proxy/pull/60) 提供流式截断与停滞的实测数据，以及 [@ck629](https://github.com/ck629) 报告 [#61](https://github.com/wnddd839/buddy-proxy/issues/61) 自更新下载中断。

`gofmt` / `go vet` / `go test -race ./...` / `go build` 全绿；新增端对端回归覆盖自动签到规则保存与调度、流式截断/停滞分流、自更新下载瞬断自愈。
