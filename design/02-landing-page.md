# 02 · 产品页 Editorial 规格

**目标文件**：`go-codebuddy/docs/index.html`  
**依赖**：`README.md` token 与 `01` 同源。  
**性质**：视觉按杂志封面**重写**；技术事实与下载表必须正确。

---

## §0 不要做的

- 不要沿用深色 `#0e1110` + 珊瑚/薄荷  
- 不要沿用 Canvas 光柱 / 粒子 / glyph rain（旧 04 作废）  
- 不要 Google Fonts / Fraunces / Sora / Inter  
- 不要 STYLEKIT 模板里的 `bg-[#e63946]` 红 hero  
- 不要把「赋能 / 一站式 / 极致 / 无缝 / 全新 / 打造」写进文案  
- 不要改下载文件名、仓库 URL、端口、路径等事实

---

## §1 页面结构（杂志骨架）

保留信息分区，换成 Editorial 版式：

```
nav          固定顶栏，cream/90 + 8px blur，衬线字标 CODEBUDDY
hero         超大衬线标题 + italic 副题 + 两个 CTA（反相主按钮 + 下划线次按钮）
why          12 栏网格：左 label / 右正文。不要 01/02/03 彩虹编号卡
download     系统 × 文件名 细线表，border-b /10
start        三步，uppercase label + 衬线序号 01 02 03（单色即可）
cta          大衬线句 + GitHub / Releases 链接
footer       反相底 #1C1C1C 字 #F9F8F6，xs uppercase · 分隔
```

产品页 **可以使用** 大留白：`padding: 96px 0` / `md: 160px 0`。  
容器：`max-width: 1280px; padding: 0 24px` / `md: 0 48px`。  
正文栏：`max-width: 28–36rem`，不要通栏灌字。

### Hero 标题建议

不要再写一句空的口号。沿用现有诚实句，放大排版：

```
把 CodeBuddy
变成 /v1.
```

副题 italic `/60`：协议直连、OAuth、账号池、OpenAI 兼容。  
主 CTA：Releases。次 CTA：GitHub。

1366×768：**标题 + 副题 + 两个 CTA 不滚动可见**。超大 `9rem` 若把 CTA 挤出首屏，降到 `clamp(3rem, 8vw, 6.5rem)`，**功能优先于模板字号**。

---

## §2 必改

1. **Token 全面替换为 cream / ink**  
2. **删除** `#ambient` Canvas 及其 JS；`prefers-reduced-motion` 不再需要藏 Canvas，只关 underline/italic  
3. **删除** Google Fonts、`.noise`、`feTurbulence`、多层彩色 radial  
4. **Logo**：`<img src="./logo.svg">` 指向重写后的单色 SVG（见 04）  
5. **og:image** 仍指向 Pages 上的 `logo.svg`  
6. 链接 hover-underline（`scaleX` 从右到左）  
7. 标题 `group-hover:italic` 仅用于列表项，hero 主标题保持静止  
8. 细分割线 `/10`，hover `/40`

---

## §3 明确保留（事实层）

- 仓库：`wnddd839/buddy-proxy`  
- Pages：`https://wnddd839.github.io/buddy-proxy/`  
- 端口 `32126`，路径 `/v1` `/direct-admin/` `/health`  
- 下载名：`codebuddy-proxy-windows-x64.exe` 等  
- 文案风格：具体、给工程师，零营销黑话  
- 协议直连 / OAuth / 号池 / Credits / 国内国际 这些事实句

---

## §4 跨页一致性

| Token | 产品页 | 管理台 | 落地页 |
|---|---|---|---|
| `--bg` | `#F9F8F6` | `#F9F8F6` | `#F9F8F6` |
| `--fg` | `#1C1C1C` | `#1C1C1C` | `#1C1C1C` |
| 标题字体 | 系统衬线 | 系统衬线 | 系统衬线 |
| 圆角 / 阴影 | 无 | 无 | 无 |

用户从 Pages 点进本地管理台，应觉得是同一份印刷品，只是 denser。

---

## §5 文档同步（产品页改完必须做）

只改描述，不改协议：

| 文件 | 动作 |
|---|---|
| `README.md` | 徽章/措辞与产品页一致；logo 仍 `docs/logo.svg` |
| `docs/README.md` | 如需，加设计规格入口 `../design/README.md` |
| `docs/guides/getting-started.md` | Windows 推荐文件名与 README 一致（若仍写 `windows-amd64`） |
| `CHANGELOG.md` | Unreleased：管理台/产品页/Logo 改为 Editorial 单色杂志风 |

禁止借文档改 env 默认值或路由。

---

## §6 验收

```bash
grep -nE "fonts.googleapis|feTurbulence|<script src=" docs/index.html   # 空
grep -nE "#e88a4a|#5ec4a8|#e63946|#0e1110" docs/index.html              # 空
grep -n "id=\"ambient\"" docs/index.html                                 # 空
cd go-codebuddy && go build ./... && go test ./...                       # 仍绿
```

人工：

1. 断网打开产品页，衬线降级不塌  
2. 1366×768 首屏 CTA 可见  
3. 与管理台并排，奶油/软黑一致  
4. 移动端无横向溢出  
5. 所有链接有可见 focus  
6. 一眼能看成 Editorial，而不是 Dashboard 模板  
