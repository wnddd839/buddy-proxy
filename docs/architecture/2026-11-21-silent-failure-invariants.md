# 静默失效不变量体系 — 设计文档

- 日期：2026-11-21
- 状态：待评审（已过一轮 builder check，见 §15）
- 仓库：`wnddd839/codebuddy-proxy`（线上 `/opt/buddy-proxy`，源码 `/home/ova/cbp-audit`）

---

## 1. 意图

用户的原问题不是「修这五个 bug」，而是「**还有多少类似的现象**」。

那次审计本身是一次性的：六个 Python 脚本写在 `/tmp`，跑完即弃，仓库里什么都没留。所以下次再问同一个问题，仍然要从零扫一遍。

要的是**机制**：

- 新缺陷出现时当天可见，而不是靠人追
- 新想到一类失效时，能立刻回放历史，判断「以前有没有中过」
- 每次代码改动都有自动化边界检查，挡住回归

## 2. 已确认的失效类别

| 事件 | 表现 | 为什么静默 |
|---|---|---|
| 额度后门 | 免费模型成功 → `clearQuotaCache` 把额度清成 nil → 永久放行 + 停止记账 | 无日志、无告警，请求 200 |
| dsh 失忆 | 同一会话 `promptTokens` 从 477,680 掉到 317,173 | 请求 200，上游确实处理了 48 万，无人比对 |

共同点：**数据里出现了不该出现的变化，但没有任何东西去比对。**

额度那次更糟：选号跳过原因根本没落盘，数据层就是瞎的——连「是谁被跳过、为什么」都答不出来。

## 3. 目标 / 非目标 / 反目标

### 目标

- **G1** 一份可增长的断言清单，对**真实数据**做检查
- **G2** 在线执行：命中即 WARN + 落盘 + 面板可见，带去重
- **G3** 离线回放：同一份清单跑历史数据，回答「以前有没有发生过」
- **G4** 把既有 5 项整改吸收成清单条目，而不是并行维护两套

### 非目标

- 不发邮件、不接 alertd、不做 metrics 端点（避免重复造监控）
- 不做自动修复（发现归发现，处置仍由人决定）
- 不改请求行为（不因为命中就拒绝 / 重试 / 换号）

### 反目标（即使「能用」也算失败）

- 面板出现大量误报，导致没人再看它
- 为检查引入 >1ms p99 延迟或 >2MB RSS
- 断言判定依赖会被上游随时改写的文案（字符串匹配）

## 4. 约束

- 进程 RSS < 80MB；body 上限 256MB 不动
- 不得打断 `:32126` 上承载的会话（部署需重启，安排在用户确认的时点）
- 正确性优先于 CPU：宁可少算，不可误报
- 复用仓内已有模式（`systemprompt` 的按日 jsonl + `Prune`、`usagejournal` 的面板展示），不引入新存储引擎
- 所有新增字段向后兼容（`omitempty`，旧数据可读）

## 5. 核心原则

**P1 断言的是数据，不是代码。**
写成「这段代码应该怎样」，只能测逻辑；写成「这组数据不该长这样」，才能测现实。dsh 那次代码完全合法，只有数据能暴露问题。

**P2 同一份清单，两个执行器。**
在线执行器看未来，离线执行器看过去。清单只有一份，维护成本不是两份。

**P3 每条断言必须能证明「这是 bug」，而不只是「这不寻常」。**
不能自证的不变量会退化成噪音。每条都要写清：断言什么、为什么违反即证明有缺陷、需要什么数据。

**P4 只有「进出不一致」才是 bug；「会话变短」不是。**
这是评审后确立的关键分界：
- **网关内部**把历史弄丢了 ⇒ 一定是 bug，可以告警
- **客户端**发了更短的上下文（压缩 / 新会话 / 标题改写）⇒ 合法行为，只能观测，不能告警

## 6. 备选方案

| 方案 | 内容 | 评价 |
|---|---|---|
| **A 运行时不变式层** | 在 journal 上挂断言，命中即告警 | 唯一能看见生产真实发生的事；但只能覆盖已编码的断言 |
| **B 离线穷举测试** | 对纯函数做全状态空间枚举，进 CI | 零生产风险；但看不见「代码合法、数据异常」这一整类 |
| **C 结构审计工具** | 六个一次性脚本变成 `cmd/audit`，AST 扫孤立字段 / 双实现 | 可重复回答结构层问题；但只报模式不报故障，噪音大 |

**已选 A + B，共享一份清单。** C 暂缓：结构层已手动扫过一遍，做成常驻工具收益低于维护成本。

## 7. 架构

### 7.1 数据层

新增按日文件 `proxy-decisions-YYYYMMDD.jsonl`，每行一条 Decision，**三种 `kind`**：

```json
{"kind":"request","at":1763700000000,"requestId":"...","sessionKey":"...","accountId":"...","site":"global","model":"deepseek-v4.1-flash","routedModel":"","promptTokens":477680,"completionTokens":3210,"usageSource":"actual","canceled":false,"titleRewrite":false,"systemFp":"ab12cd34","systemLen":42001,"systemForm":"keep","stripped":false,"stripScope":"","degraded":false,"inMessages":12,"outMessages":12,"inBytes":48211,"outBytes":48211,"ok":true,"errorCode":""}
{"kind":"selection","at":1763700000000,"requestId":"...","site":"global","model":"...","candidates":4,"chosen":"f53d...","skipped":{"quota_floor":3,"cooldown":2}}
{"kind":"quota","at":1763700000000,"accountId":"...","op":"deduct","before":12.5,"after":11.2,"credit":1.3,"spentSinceCheck":8.4,"checkedAtBefore":1763699000000,"checkedAtAfter":1763700000000,"reason":""}
```

**为什么不复用 `proxy-usage.json`**：后者是 UI 用途，内存里只保留 400 条明细，历史深度不够。jsonl 是流式追加，无内存压力。

**体量实测与估算**

- `systemprompt` jsonl 实测（11-18 ~ 11-20）：每日 **638 / 1,613 / 1,949** 条请求，平均 **~299B/行**
- 本方案三种 kind 各约 1 行/请求 ⇒ 约 **4,000 行 / 天 × ~200B ≈ 0.8MB/天**，30 天约 **24MB**
- 磁盘现状：`/` 已用 78%（剩 25G），可忽略

**保留**：30 天，每日单文件。

**复用**：`internal/systemprompt/store.go` 已实现「按日文件 + retention + `Prune`（`storeDateLayout=20060102`）」。把该存储抽成通用按日 jsonl writer 复用，不另写轮转逻辑。

### 7.2 执行器一：在线

三个挂点：

| 挂点 | 产出 |
|---|---|
| `internal/gateway/usage_record.go` | `kind=request` |
| 选号路径（`selectFloor` 及指定账号分支） | `kind=selection` |
| `internal/accounts/pool.go` 额度写入处 | `kind=quota` |

流程：

```
生成 Decision
  → 同步跑「无状态断言」（纯算术 / 计数比较，O(1)）
  → 同步更新 Memo（仅会话级观测用，O(1)）
  → 有 Finding 则投递到有界 channel
      → 单 goroutine 落盘 + 推入面板 ring
```

- **Memo**：键为 `(sessionKey, accountId, model)`，只存 `promptTokens` / `at`。LRU 上限 512，超限淘汰最旧。**只服务于 info 级观测，不参与任何 violation 判定**
- **channel**：容量 1024，满则丢弃并累加 `droppedDecisions`（绝不阻塞请求）
- **去重**：同一 `(invariant, subject)` 10 分钟内只落一条，附 `suppressed` 计数
- **安全**：断言调用全部包 `recover()`；panic 只记一条 WARN，不影响请求

### 7.3 执行器二：离线

独立二进制 `cmd/invariant-audit`（不进主二进制）：

```
invariant-audit -dir /opt/buddy-proxy [-since 2026-08-01] [-until ...] [-only I3,I5] [-json]
```

- **输入**：`proxy-decisions-*.jsonl` + `proxy-usage.json`（补部署前最近 400 条）+ `proxy-systemprompt-*.jsonl`（更长历史）
- **输出**：按不变量分组、按 subject 聚合，含首次 / 末次命中时间与总次数
- **能力边界**（必须打印）：部署前历史只能覆盖最近 400 条请求，更早仅 system 类可用

### 7.4 清单 v1

| ID | 断言 | 类型 | 数据 | 为什么违反即证明有 bug |
|---|---|---|---|---|
| **I1** `quota.no_silent_unknown` | 余额 `非 nil → nil` 仅允许 `op=import` 或 `op=reset` 且 `reason ∈ 常量白名单` | 转移 | quota | nil 语义是「未知 → 放行」，无理由清空等于撤销保护 |
| **I2a** `quota.deduct_mismatch` | 同一请求里，`pool` 实际扣减额必须等于上游 `Usage.Credit`（±1e-6） | 转移 × 请求 | quota + request（按 requestId 关联） | 上游收 1.3 而我们扣 0，就是漏记 |
| **I2b** `quota.probe_drift` | `refresh` 时 `before-after` 与已持久化的 `QuotaSpentSinceCheck` 偏差 >0.5 credit | 转移 | quota | `ApplyQuotaProbe` 已做 in-flight 校正，仍有偏差说明账本漏记（info 级） |
| **I3** `inout.history_shrunk` | 出站消息条数 < 入站条数 − 预期变化量 | **进出对比** | request | 网关在两个方向之间减少了历史，且没有对应策略记录 |
| **I3b** `session.context_drop` | 同 `(sessionKey, accountId, model)` 的 `promptTokens` 环比降 >25%，且上次 >50k，且两次都 `canceled=false` 且 `usageSource=actual` | 观测 | request + Memo | **只记 info，不告警**。用于回答「dsh 那种下降发生在什么时候、多少次」 |
| **I4** `inout.system_vanished` | 入站有 system（len>0）且出站 system 为空 | **进出对比** | request | 合法仅当 `form=fold` / `stripScope∈{system,both}` / `degraded`；否则是无声移除 |
| **I5** `request.error_unclassified` | 失败请求落不到 `errorCode` | 观测 + 阈值 | request | 单条不告警；同一错误文案 24h 内 **≥3 次**仍 `unclassified` 才判 violation（出现持续性未知类别） |
| **I6** `quota.predicate_agreement` | 4 个额度判定函数在同一状态下结论一致 | **仅离线** | 纯函数穷举 | 同一概念两套答案，已知会造成后门 |
| **I7** `selection.skip_unexplained` | 有账号被跳过但 `skipped` 直方图为空；或 `chosen==""` 且直方图总和 ≠ `candidates` | 转移 | selection | 跳过原因缺失 = 无法回答「那个号为什么没被用」 |
| **I8** `request.model_substitution` | 上游回显 `model` ≠ 请求 `model` | 观测 | request | 模型被偷换，所有行为分析失效（**Phase 2**） |

**I3 / I4 的预期变化量**（这是本设计的关键，避免误报）：

| 策略 | 条数变化 | 字节变化 |
|---|---|---|
| `fold` | system 条 −1 | system 字节移入 user |
| `strip`（`stripScope=system`） | 不变 | 减少 `stripSystemBytes` |
| `strip`（`stripScope=assistant`） | 不变 | 减少 `stripAssistantBytes` |
| `echo`（实验开关） | 不变 | **增加** system 字节（user 里多一份） |

**I3/I4 的豁免清单**（必须在记录时打标，否则无从判断）：
`titleRewrite=true`（标题改写请求形状特殊）｜`canceled=true`（取消请求 usage 全 0）｜`usageSource=estimated`（估算值不可比）｜图片请求

每条在注册表里必须带 `whyThisIsABug` 字段，面板 tooltip 直接展示。没有该字段的条目不允合入。

### 7.5 面板

overview 页新增卡片「静默失效体检」：

- 近 24h 按不变量计数（含 `suppressed` / `droppedDecisions`）
- 最近 20 条明细：时间 / 不变量 / 对象 / 一句话说明
- 右上角标注数据覆盖范围，格式「自 <日期> 起」

**跨重启**：启动时回扫**当天 + 前一天**的 decisions jsonl 重建计数（单文件 <1MB，代价可忽略）。不允许出现「重启后显示 0 命中、与确实没发生无法区分」——那正是本体系要消灭的静默。

API：`GET /direct-admin/api/invariants?limit=20`（只读，无写接口）。

## 8. 需要新增 / 修改的字段

| 位置 | 改动 | 理由 |
|---|---|---|
| `usagejournal.Entry` | +`SystemLen`、+`Stripped`/`StripScope`、+`ErrorCode`、+`UsageSource`、+`Canceled`、+`TitleRewrite`、+`InMessages`/`OutMessages`/`InBytes`/`OutBytes` | I3 / I4 / I5 与豁免判定 |
| `accounts.Pool` | +`SetQuotaObserver(QuotaObserver)` 接口 | 让 accounts 不依赖 invariant 包（避免循环引用） |
| `pool.go` 写入点 | 发 `quota` Decision；`reason` 用**编译期常量** | I1 / I2 |
| 选号路径 | 发 `selection` Decision，带跳过原因直方图 | I7 |
| `gateway` | 观测器接线 + Decision 写入器 | 执行器一 |

**`op` 全集**：`deduct | clear | refresh | import | reset`。
**`reason` 白名单**（编译期常量 + 启动自检，未知或空值在 `非 nil → nil` 时判 violation）：
`probe_expired`｜`admin_clear`｜`account_reimport`｜`model_unsupported`

**不是 transition 的写入**：库存载入 / `normalize` / 服务层字段回写（`gateway/service.go:1047-1050` 这类 hydration）不发 Decision——它们不是状态变化。

## 9. 预算

| 项 | 上限 | 说明 |
|---|---|---|
| 常驻内存 | +2MB | Memo 512 会话 + ring 200 条 + channel 1024 |
| 每请求延迟 | +0.1ms | 同步部分只有计数比较与 map 查改 |
| 磁盘 | +0.8MB/天 | 实测日均 <2,000 请求 × 3 类 Decision；30 天约 24MB |

## 10. 测试策略

- 每条不变量至少 2 个用例：**命中** 与 **不命中（对照）**，后者防止断言恒真
- I3/I4 必须覆盖**每一种策略组合**（fold / strip-system / strip-assistant / echo / 无 system / 标题改写 / 取消），逐组合断言「不报」
- Memo 边界：1000 个会话不超 512、淘汰最旧、并发安全（`-race`）
- 去重：10 分钟内同 subject 只落 1 条，计数正确
- 回放：构造 3 天数据跑 `invariant-audit -json`，断言条数与分组
- I6 穷举：`rem ∈ {nil, 0, 小, 大, ∞} × checkedAt ∈ {0, 新, 陈旧} × resetAt ∈ {0, 已过, 未来}` 全枚举，断言 4 个函数一致；同时保留 PR66 的「nil 放行」语义
- 全量 `go test -race` 必须绿（CI 已有）

## 11. 与既有 5 项计划的关系

| 原计划 | 在新体系里的位置 |
|---|---|
| P1 统一额度判定 | I6 的穷举测试就是验收标准；统一后的判定函数成为 oracle |
| P2 收窄关键词 | **Phase 1.5**：先收一周 I5 基线，再拿真实未分类文案改分类器 |
| P3 选号失败带原因 | 升级为 `selection` Decision + I7 |
| P4 状态突变告警 | 即本体系在线执行器，不再单独实现 |
| P5-B 额度漂移对账 | I2a（每请求对账）+ I2b（探针漂移） |

## 12. 分期

**Phase 1（本次）**

- 数据层（三种 Decision）+ 两个执行器 + I1 / I2a / I2b / I3 / I3b / I4 / I5 / I7 + I6（离线穷举）
- 面板卡片 + 启动回扫
- 前置：P1（统一判定）、P3（跳过原因）一并完成

**Phase 1.5**

- 依据一周 I5 基线，收窄 `isQuotaExhausted` 关键词并补回归用例

**Phase 2**

- I8 上游回显抓取（改 SSE 解析层）
- 离线回放跨源关联（用 `systemprompt` jsonl 补 `SystemLen`）

## 13. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 断言误报把面板刷成噪音 | 首周只 WARN；人工复核命中率；误报的断言直接下掉，不调阈值 |
| 在线检查影响请求 | 同步部分只做内存操作；异步落盘有界丢弃；panic 兜底 |
| 磁盘增长 | 30 天保留 + 每日单文件；超 200MB 复用现有磁盘检查 |
| 部署需重启 | 安排在用户确认时点，避开活跃会话 |

回滚：单二进制备份切换；决策日志只增不改，旧二进制忽略新文件即可。

## 14. 留给实现者的决定

- Decision 的字段命名与 JSON tag
- Memo 的 LRU 实现（手写 ring vs 现成容器）
- 面板卡片排版
- `invariant-audit` 报告格式（表格 vs JSON）
- ring 容量、去重窗口、漂移阈值、I5 的「≥3 次」阈值（本文件给了默认值）

## 15. 评审修正（builder check 后）

评审提出 8 个会导致误报或空转的问题，处理如下：

| # | 问题 | 处理 |
|---|---|---|
| 1 | 标题改写 / 取消 / 估算 usage / 客户端压缩会让 I3/I4 每会话必命中 | **I3/I4 改为「进出对比」**（网关内部是否丢失），新增 `titleRewrite`/`canceled`/`usageSource` 标记与豁免清单；会话级下降降为 I3b，仅 info |
| 2 | I7 在 §7.1 没有对应 kind，粒度不明 | 新增第三种 kind `selection`，每次选号 1 行带原因直方图（不是每账号 1 行） |
| 3 | I5 照字面会每天几千条误报（多数失败本来就没码） | 改为 info + 阈值升级：同一文案 24h ≥3 次且仍 unclassified 才判 violation；明确豁免集 |
| 4 | I2 的 deduct 分支恒真；refresh 没有可比账本 | deduct 改为**与上游 `Usage.Credit` 对账**（跨 request/quota）；refresh 用**已持久化**的 `QuotaSpentSinceCheck` 对账，确认为 `json:"quotaSpentSinceCheck"` 真的落盘 |
| 5 | I4 未定义合法清空路径 | 列出全部豁免：`fold` / `stripScope` / `degraded` / 标题改写 / 取消 / 无 system |
| 6 | I1 的 `reason` 若是自由字符串则形同注释 | `reason` 改编译期常量白名单 + 启动自检；明确 `op` 全集与「hydration 不算 transition」 |
| 7 | 面板计数只在内存，重启后 0 与「没发生」不可区分 | 启动回扫当天 + 前一天 jsonl 重建计数 |
| 8 | §11 说 P2 独立做、§12 又说 Phase 1 含 P2，自相矛盾 | P2 移到 **Phase 1.5**，先收一周基线再改 |
