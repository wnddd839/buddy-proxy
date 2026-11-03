## v0.7.1 · 2026-11-03

### 修复

- 普通号池不再通过“全冷却降级”绕过当前模型的 6004 / `rate-model` 冷却。全部候选受限时立即失败，不在服务端长时间排队；其他模型仍可用，到重置时间后恢复。显式固定账号 Key 保留直接请求语义。
- 支持中文“将在…重置”时间解析，避免有效的重置时间回退成固定两分钟。
- Chat 非流式冷却错误提供 `Retry-After` 响应头与 `error.retry_after` 秒数；已打开的 SSE 通过错误事件提供 `retry_after`，保留既有 HTTP 错误状态约定。

### 可选部署配置

```env
# 公开推理接口 JSON 请求体上限，单位 MiB，默认 64
CODEBUDDY_PROXY_MAX_BODY_MB=64
# HTTP keep-alive 空闲超时，单位毫秒，默认 120000
CODEBUDDY_PROXY_SERVER_IDLE_TIMEOUT_MS=120000
```

修改后重启生效。JSON 上限按服务实例应用，超限返回 413 并显示实际上限；管理接口限制不变。调大前请核对反代限制和可用内存，不会扩大模型上下文窗口。空闲超时不控制活跃 SSE 或客户端等待响应头的时间。

本版不全局关闭 SSE 连接复用，也不改变账号出口策略；特定隧道超时和出口回显站不可达仍需部署侧证据，不将其宣称为已修复。

### 升级

保留现有 `.env`、账号池、`proxies.json` 和用量文件，替换对应平台二进制，或在管理台手动检查更新。七个平台二进制附 `SHA256SUMS.txt`；`default.env.example` 仅供参考，不要覆盖已有配置。

Docker 用户待对应标签的 Docker release 工作流成功后使用 `ghcr.io/wnddd839/buddy-proxy:v0.7.1`，或按原流程更新 `latest` 镜像。保留数据卷，不要执行 `docker compose down -v`。

### 贡献与验证

感谢 [@240xu](https://github.com/240xu) 提供 [#52](https://github.com/wnddd839/buddy-proxy/pull/52)、[#53](https://github.com/wnddd839/buddy-proxy/pull/53) 配置方案与 [#54](https://github.com/wnddd839/buddy-proxy/issues/54) 限流实测反馈。修复与适配合并于 [#55](https://github.com/wnddd839/buddy-proxy/pull/55)。

全量测试、race tests、vet 和核心 HTTP 回归通过；消融移除冗余包装、重复检查及一次性调查用例，不增加排队调度或指标系统。
