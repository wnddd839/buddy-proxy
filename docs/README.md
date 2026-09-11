# Documentation Index

Buddy Proxy 文档目录（CodeBuddy / WorkBuddy 协议网关）。

## 全局

| 文档 | 说明 |
|------|------|
| [../README.md](../README.md) | 子项目总览与快速开始 |
| [guides/getting-started.md](guides/getting-started.md) | 安装、启动、首次 OAuth、验证 |
| [guides/configuration.md](guides/configuration.md) | **全部环境变量、默认值、`.env` 查找顺序、旧版兼容名** |
| [architecture/overview.md](architecture/overview.md) | 架构、包职责、请求链路、并发模型 |
| [api/http.md](api/http.md) | Public `/v1` 接口与 Admin API |
| [operations/runbook.md](operations/runbook.md) | 运行、排障、持久化、资源占用 |
| [standards/coding-standards.md](standards/coding-standards.md) | 本仓库编码规范 |
| [standards/modern-go-guidelines.md](standards/modern-go-guidelines.md) | JetBrains 现代 Go 指南摘录（Go ≤ 1.26） |
| [../design/README.md](../design/README.md) | 前端 Editorial 设计规格总纲（管理台 / 产品页 / Logo） |

## 仓库级

| 文档 | 说明 |
|------|------|
| [../../README.md](../../README.md) | 项目主页（特性、接入、国内/国际、CodeBuddy/WorkBuddy） |
| [../../CHANGELOG.md](../../CHANGELOG.md) | 更新日记 |
| [../../SECURITY.md](../../SECURITY.md) | 漏洞报告说明 |
| [../releases/README.md](../releases/README.md) | 预编译包下载与平台对照 |

## Vendor / Skills

| 路径 | 说明 |
|------|------|
| `.agents/skills/use-modern-go/` | JetBrains 官方 skill（含 CLI wrapper） |
| `.agents/skills/codebuddy-go/` | 项目级 skill |
| `standards/vendor/` | JetBrains FEATURES / LICENSE / guidelines.json 原文归档 |

## 阅读顺序建议

1. `guides/getting-started.md`
2. `guides/configuration.md`
3. `architecture/overview.md`
4. `api/http.md`
5. 需要改语法时加载 `use-modern-go` skill

## 文档同步约定

变更下列内容时必须更新 `docs/`：

- 路由（→ `api/http.md`）
- 环境变量（→ `guides/configuration.md`）
- 包边界（→ `architecture/overview.md`）
- OAuth / 账号池行为（→ `architecture/overview.md` + `operations/runbook.md`）
