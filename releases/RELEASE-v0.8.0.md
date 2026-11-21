# Buddy Proxy v0.8.0

## v0.8.0 · 2026-11-21 · 全新管理台 UI（Atelier / Swiss 四主题）与 90 天历史全览

这是一次围绕「好看、顺手、长期可维护」做的里程碑式升级：我们把沿用至今的 4,000 行手写内联管理台彻底推倒，用 **Vite + React 19 + TypeScript + Tailwind CSS v4** 重新打造了一套全新控制台，同时继续坚守「单文件二进制、零外链 CDN、离线秒开」的初衷。

### 亮点与新功能

- **全新管理台 UI 架构（单文件内嵌）**：
  - 彻底移除 `internal/admin/page.go` 中近 4,000 行手写拼接 HTML/CSS/JS，迁移至 `web/` 模块化前端工程，构建产物自动打包为自包含单文件 `internal/admin/dist/index.html` 嵌入 Go 二进制；不引入任何外部字体或 CDN 请求，断网和内网环境照常秒开。
  - 同步焕新 OAuth 回调落地页样式，自动跟随系统深浅色并与新工作台视觉统一。
- **四套精调双系列主题随心切**：
  - 内置 **温润纸刊 · 亮**（`atelier-light`）、**暗墨工坊 · 暗**（`atelier-dark`）、**精密冷白 · 亮**（`swiss-light`）、**深邃黑曜 · 暗**（`swiss-dark`）四套主题；首次访问自动匹配系统深浅色，手动切换后本地持久化。
- **更克制、更清晰的信息架构与 Hero 趋势图**：
  - `md (>=768px)` 起左侧分组导航常驻固定、右侧内容独立滚动；顶部状态栏瘦身为 48px 轻量条，国内 / 国际与 CodeBuddy / WorkBuddy 切换高对比度、不再折行；次要说明移入侧滑抽屉，自更新器常驻侧栏底部。
  - **统计总览重构**：告别压迫感卡片墙，改为顶栏扁平细线指标条 + 全宽平滑单调曲线面积图（Fritsch-Carlson Monotone，展示 Token 与缓存命中趋势）+ 无边框双栏摘要。
  - **账号池与用量页体验整合**：账号池页直接汇总双站计数、Credits 余额、健康状态与问题筛选；请求明细页复用同一套趋势折线图。
- **支持「全部」历史范围查询（最长 90 天）**：
  - 后端 `usagejournal` 与 `gateway` 新增 `range=all` 查询支持，总览页与请求明细页同步新增「全部」时间档位，超过 30 天的保留记录现在也能完整回看趋势图与明细。

### 边界修复与运维增强

- **90 天历史保留边界精确裁切**：统一本地日历日 90 天保留起点（`retentionStart`），加载旧版持久化快照时，`range=all` 与按账号/模型筛选均严格排除超期条目，避免过期的历史记录拉长趋势图跨度或混入筛选统计。
- **Linux systemd 安全更新脚本**：新增 `scripts/update-linux-systemd.sh`，通过目录文件锁防并发更新，支持自动备份旧二进制、原子替换、重启后轮询 `/health` 校验，以及启动异常时自动回滚至备份版本。
- **工程协作与测试体系升级**：
  - 前端核心领域逻辑校验迁移至 Vitest，Playwright 浏览器端到端回归覆盖固定侧栏、Hero 折线图路径渲染、多主题与多端布局；
  - 新增 `CONTRIBUTING.md` 明确开发与本地验证规范，同步修复 `.githooks/pre-commit` 对根目录 Go 模块的 `gofmt` 与受影响包增量测试。
- **新版 README**：全面重写项目首页介绍，重点展示 v0.8 新控制台体验并精简上手路径。

### 验证

`gofmt`、`go vet ./...`、`go test -race -count=1 ./...` 以及前端 `npm run typecheck`（`tsc --noEmit`）、`npm test`（Vitest）全部通过。

### 升级

直接替换对应平台二进制、在旧版管理台点击「检查更新 / 更新并重启」，或更新 Docker 镜像（`ghcr.io/wnddd839/buddy-proxy:v0.8.0`）。不覆盖 `.env`、账号池、签到配置与历史用量文件，平滑兼容 v0.7.x 数据。

## 下载与校验

提供 Windows x64、Linux amd64/arm64/armv7/armv6、macOS arm64/amd64 七种二进制，随附 `SHA256SUMS.txt` 和 `default.env.example`。请校验 SHA-256；示例配置仅供参考，不要覆盖现有配置。Docker 镜像为 `ghcr.io/wnddd839/buddy-proxy:v0.8.0`（标签推送后由 GitHub Actions 构建发布）。
