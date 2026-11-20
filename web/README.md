# Buddy Proxy Admin Console (`web/`)

基于 **Vite + React 19 + TypeScript + Tailwind CSS v4** 构建的单文件自包含管理控制台，编译产物打包至 [`internal/admin/dist/index.html`](../internal/admin/dist/index.html)，由 Go 标准库 `//go:embed` 直接打入单一可执行文件（零外部 Go 依赖、零 Web Font CDN、100% 离线运行）。

## 开发与构建

```bash
# 1. 本地热更新开发（自动代理 /direct-admin/api/* 到 127.0.0.1:32126）
npm --prefix web run dev

# 2. 运行核心领域逻辑单元测试（Vitest）
npm --prefix web run test

# 3. 类型检查并构建单文件产物到 internal/admin/dist/index.html
npm --prefix web run build

# 4. 构建 Go 单二进制（不依赖 Node）
go build ./cmd/codebuddy-proxy
```

## 设计系统与防漂移约定（新增功能必读）

为防止长期迭代中出现视觉风格割裂、文档墙或布局溢出，所有新页面与组件**必须**遵守以下约束：

1. **禁止硬编码颜色**：一律使用 [`src/styles/tokens.css`](./src/styles/tokens.css) 中的语义变量（`--canvas`、`--sidebar`、`--surface`、`--surface-2`、`--border`、`--fg`、`--fg-2`、`--fg-3`、`--accent`、`--ok`、`--warn`、`--err`、`--info`），确保 4 套皮肤（Atelier 亮/暗、Swiss 亮/暗）无缝适配。
2. **优先拼装标准原语**：页面必须复用 [`src/components/ui/`](./src/components/ui/) 下的基础组件：
   - `PageHeader`：统一页头结构与操作按钮位；
   - `Card` / `StatCard`：统一表面材质、圆角与等宽数字排版（`.ds-mono`）；
   - `StatusBadge`：统一语义状态色胶囊；
   - `SegmentedControl`：统一分段切换器；
   - `SlideDrawer`：长篇机制说明、原始 JSON 诊断、低频运维策略一律收纳进按需滑出抽屉，**禁止在首屏堆叠大段说明文字**；
   - `Modal` + `useConfirm()`：统一确认框，**禁止调用浏览器原生 `window.confirm()`**；
   - `DirtySaveBar`：所有带草稿的配置页在有未保存改动时使用底部统一保存条。
3. **禁止引入外部 CDN**：不得引入 Google Fonts、外链图标库或外部脚本，图标统一通过 `lucide-react` 按需内联 SVG。
4. **纯逻辑下沉单测**：业务计算与状态分类函数放入 [`src/lib/domain.ts`](./src/lib/domain.ts)，并在 [`src/__tests__/domain.test.ts`](./src/__tests__/domain.test.ts) 补齐 Vitest 断言。
