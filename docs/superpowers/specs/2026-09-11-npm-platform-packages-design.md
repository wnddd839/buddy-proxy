# npm 按平台分发设计

日期：2026-09-11  
状态：待实现  
范围：把现有 GitHub Release 四份 Go 二进制，额外发到 npm，方便 `npm i -g` / `npm update -g`。

## 目标

用户可以：

```bash
npm i -g buddy-proxy
buddy-proxy
npm update -g buddy-proxy
```

只下载当前操作系统对应的那一份二进制。国内 npm 镜像可用。GitHub Releases 继续保留，行为不变。

非目标：

- 不把网关改成 Node 实现
- 不新增 linux-arm64 / windows-arm64（与现有 Release 矩阵一致）
- 不提供 `codebuddy-proxy` 命令别名（需要时再加）
- 不把 `.env` 写进 `node_modules`（升级会丢）

## 包与版本

公开名（均未占用，已用 `npm view` 确认 404）：

| npm 包 | npm `os` / `cpu` | 内含文件 |
| :--- | :--- | :--- |
| `buddy-proxy` | 不限 | Node 包装器 + `bin` |
| `buddy-proxy-windows-x64` | `win32` / `x64` | `codebuddy-proxy.exe` |
| `buddy-proxy-linux-amd64` | `linux` / `x64` | `codebuddy-proxy` |
| `buddy-proxy-darwin-arm64` | `darwin` / `arm64` | `codebuddy-proxy` |
| `buddy-proxy-darwin-amd64` | `darwin` / `x64` | `codebuddy-proxy` |

版本与 GitHub tag 对齐：`v4.5` → `4.5.0`。无预发布后缀时，`vX.Y` 发成 `X.Y.0`，`vX.Y.Z` 发成 `X.Y.Z`。五包必须同版本一次发齐。

主包 `optionalDependencies` 精确钉死四个平台包的同一版本。平台包设置 `os` + `cpu`，npm 不会装错平台。

许可证 `BSD-3-Clause`。`repository` / `homepage` 指向 `https://github.com/wnddd839/buddy-proxy`。

## 仓库布局

全部放在产品根 `go-codebuddy/npm/`（随 subtree 发布）。Go 源码与 `make release` 不依赖 Node。

```text
npm/
  buddy-proxy/
    package.json
    bin/buddy-proxy.js      # #!/usr/bin/env node
    lib/resolve-binary.js   # 按 process.platform / arch 解析路径
    lib/resolve-binary.test.js
  templates/
    platform-package.json   # 平台包模板（name/os/cpu 由发布脚本填）
  README.md                 # 仅说明这是发布用树，不要 npm i 这里当运行时
```

平台包目录不进 git：发布脚本从 `releases/` 拷二进制、写 `package.json`、在临时目录 `npm publish`。二进制继续不进 Git。

## 包装器行为

`bin.buddy-proxy` → `bin/buddy-proxy.js`：

1. 解析当前平台对应的 optional 包路径（`require.resolve`）。
2. 找不到：退出码 1，打印明确错误（平台不受支持，或 optional 安装被跳过），并列出支持矩阵。
3. 找到：`spawn` 该二进制，`stdio: inherit`，参数原样转发，退出码与子进程一致。Windows 用 `.exe`。
4. 若进程环境**尚未**设置 `CODEBUDDY_PROXY_ENV_FILE`，包装器设置为用户主目录下的 `~/.codebuddy/.env`（Windows 为 `%USERPROFILE%\.codebuddy\.env`）。已设置则不覆盖。

账号池默认已是 `~/.codebuddy/proxy-accounts.json`。这样 `npm update -g` 不会丢掉 Key 和号池。用户仍可用环境变量或当前目录 `.env`（Go 侧：已存在的环境变量不被 `.env` 覆盖；`CODEBUDDY_PROXY_ENV_FILE` 优先作为写入目标）。

直接下载 GitHub 二进制的用户：不经过包装器，查找顺序不变。

## 发布流程

现有「本地 `make release` + GitHub Release 上传资源」保留。

新增 GitHub Actions：`release.yml`，触发 `release: types: [published]`。

1. 从当前 Release 下载四份二进制 + `SHA256SUMS.txt`，校验 sha256。
2. 由 tag 推出 npm 版本。
3. 生成四个平台包并 `npm publish --access public`。
4. 发布主包 `buddy-proxy`（必须在四个平台包成功之后，否则 optional 会 404）。
5. 使用 repository secret `NPM_TOKEN`（npm Automation token）。未配置则 job 失败并提示，不半发。

本地可重复：`make npm-publish VERSION=4.5.0`（先 `make release`），`NPM_PUBLISH_DRY_RUN=1` 只 `npm pack` 不发布。

失败策略：任一平台包 publish 失败则中止，不发主包。已成功的平台包本次不自动 unpublish（npm 有 72h 限制且危险）；重跑前先修版本或等包可用。

## 用户文档

`README.md`、`docs/guides/getting-started.md`、产品页「下载」：在「下载即用」之上增加推荐路径：

```bash
npm i -g buddy-proxy
buddy-proxy
```

说明：需要 Node.js（用于包装器，不跑网关逻辑）；配置默认写入 `~/.codebuddy/.env`；GitHub 二进制仍可用。

## 测试

- `resolve-binary.js`：对 win32/x64、linux/x64、darwin/arm64、darwin/x64 返回正确包名；对其它组合返回明确错误。
- 包装器：未设 `CODEBUDDY_PROXY_ENV_FILE` 时注入 `~/.codebuddy/.env`；已设则保持原值。用 `node --test` 跑，不启动 Go 网关。
- CI 现有 Go 测试不变。npm 单测加进 `ci.yml` 一步（`node --test npm/buddy-proxy/lib/*.test.js`），无 Node 矩阵、不发布。

## 前置条件（人）

1. npm 账号，开通 2FA。
2. 生成 Automation token，写入 GitHub repo secret `NPM_TOKEN`（`buddy-proxy` 与镜像仓若共用 Actions，至少 origin 要有）。
3. 首次发布前确认仍能 `npm view buddy-proxy` 得到 404，避免名字被抢。

没有 token 时可以先合代码与文档；CI 发布会红，直到 secret 配好。
