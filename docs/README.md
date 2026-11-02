# Documentation Index

Buddy Proxy 文档目录（CodeBuddy / WorkBuddy 协议网关）。

## 全局

| 文档 | 说明 |
|------|------|
| [../README.md](../README.md) | 项目总览与快速开始 |
| [guides/getting-started.md](guides/getting-started.md) | 安装、启动、首次 OAuth、验证 |
| [guides/docker.md](guides/docker.md) | Docker / Compose 部署、数据卷与 GHCR 多架构 release |
| [guides/search.md](guides/search.md) | 下游 Chat / Responses 显式启用官方搜索、关闭与中转透传 |
| [guides/configuration.md](guides/configuration.md) | **全部环境变量、默认值、`.env` 查找顺序、旧版兼容名** |
| [architecture/overview.md](architecture/overview.md) | 架构、包职责、请求链路、并发模型 |
| [api/http.md](api/http.md) | Public `/v1` 接口与 Admin API |
| [operations/runbook.md](operations/runbook.md) | 运行、排障、持久化、资源占用 |
| [operations/monitoring.md](operations/monitoring.md) | 波动监测：推理缺失 / 错误率 / 耗时异常的相对基线告警 |

## 仓库级

| 文档 | 说明 |
|------|------|
| [../README.md](../README.md) | 项目主页（特性、接入、国内/国际、CodeBuddy/WorkBuddy） |
| [../CHANGELOG.md](../CHANGELOG.md) | 更新日记 |
| [../SECURITY.md](../SECURITY.md) | 漏洞报告说明 |
| [../releases/README.md](../releases/README.md) | 预编译包下载与平台对照 |

## 阅读顺序建议

1. `guides/getting-started.md`
2. `guides/configuration.md`
3. `architecture/overview.md`
4. `api/http.md`
5. `guides/search.md`
