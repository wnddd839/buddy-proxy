## v0.7.0 · 2026-11-02

### 新功能

- 可选波动监测：从真实请求观测错误率、推理字段缺失和响应耗时，支持日志、webhook、SMTP 告警与恢复通知。默认关闭，使用 `CODEBUDDY_PROXY_WATCH=on` 启用。
- 账号批量导入：支持客户端凭据、账号池文件和 `account + auth` JSON，重复账号更新，混合批次逐条显示错误。
- 账号固定出口：支持 HTTP / HTTPS / SOCKS5 / SOCKS5h，提供代理候选清单、检测、单账号绑定和均衡分配。

### 修复

- 已发出正文或工具增量后不再重放；空回合拥有独立重试预算；截断工具参数不再伪装成成功完成。
- 导入保留未提供的旧账户字段、禁用状态与出口绑定；对齐 OAuth 身份和强制站点的实际路由。
- 持续故障不再污染健康基线而误报恢复；关闭推理和主动取消不再产生推理缺失误报；SMTP 支持超时与取消。
- 搜索、模型列表和 OAuth 刷新遵循账号出口；无效配置拒绝使用，不静默回退直连；修复出口设置的并发问题与错误信息脱敏。

### 升级

保留现有 `.env`、账号池和用量文件，按原方式替换二进制，或在管理台手动检查更新。Docker 用户按镜像更新流程升级。七个平台发行包均附 `SHA256SUMS.txt`。

未绑定账号出口时保留进程默认代理环境；直连检测对照忽略环境代理。账号池及 `proxies.json` 可能包含代理凭据，请勿公开。

### 贡献者致谢

感谢 [@240xu](https://github.com/240xu) 提供 [#48](https://github.com/wnddd839/buddy-proxy/pull/48) 波动监测、[#49](https://github.com/wnddd839/buddy-proxy/pull/49) 批量导入和 [#50](https://github.com/wnddd839/buddy-proxy/pull/50) 固定出口功能。

感谢 [@kouekikin24](https://github.com/kouekikin24) 提供 [#44](https://github.com/wnddd839/buddy-proxy/pull/44) 流重试和合法截断完成保护。

本版基于以上 PR 适配整合并修复已确认问题，保留原作者贡献归属，不以是否直接合并原 PR 为条件。
