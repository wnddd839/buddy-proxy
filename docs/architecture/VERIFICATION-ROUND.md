#!/usr/bin/env python3
"""把已验证的结论写成一份可复核的文档，并提交这批修复。

## 本轮校验发现并修复的清单

### Critical（已修）

1. **07 用量表表头重复「上游模型」→ 15 th vs 13 td，整表错位**
   根因：补丁脚本重复插入。已修为 13/13，与 colspan=13 一致。
   我的第一条检查脚本自己也错了两处（`<th` 把 `<thead>` 算进去、
   `<th[ >]` 匹配不到裸 `<th>`），所以先假报了「14 vs 13」两次。

2. **采样让 tokRatio 失真 7 倍 → I9 会误报 85%**
   线上实测：tokChars=327680（采样上限）、promptTokens=691677 → 2.11，
   真实 ascii 比例约 0.31。基线已按 2.11 建立：
       deepseek-v4.1-flash|ascii fast=2.1108 slow=2.1161
   下一条真实 ascii 请求会被判偏离 85% → I9 violation 误报。

   修法（两处）：
   a. 采样时 tokRatio 返回 0（不参与 I9）
   b. 新增 Composition.TotalChars（全文字符数，精确），Ratio 改用
      TotalChars → 长上下文也能参与 I9，而不是完全失明

3. **06 面板 tooltip 说反了（我写错的）**
   systemPromptDigest 返回 `len(joined)` —— Go 的 len(string) 是**字节数**。
   我却写了「字符数，按 Go 的 len(string) 计，不是 UTF-8 字节数」，自相矛盾。
   原标签「system 字节」本来是对的，被我改错了。已改回字节数并说明关系。

### Important（已修）

4. **EstimatedTokens 死代码 + 公式错**（ASCII 全用 Alpha 权重，
   符号 0.6786 被按 0.2047 算，代码类文本低估 30%+）。已删。

5. **I9 基线不持久化，重启从零重建**
   重启后前 3 个样本即固化基线；若那 3 个恰好是采样 bug 期间的假值
   （ascii=2.11，真实 0.31），错误被当成「正常」。
   已实现 Backfill 回放最近 24h request 记录重建基线。

### 已复核确认的结论（重复实测）

| 模型 | 中文 | 英文 | 数字 | 结论 |
|---|---|---|---|---|
| deepseek-v4.1-flash | +4 | +4 | +4 | == 官方 DeepSeek 词表 |
| wb-lite | +4 | +4 | +4 | == 官方 DeepSeek 词表（同一套）|
| hy4flash | −79 | +21 | +21 | 非 DeepSeek（最大偏差 4.0%）|
| glm-5.3-flash | +112 | +12 | +812 | 非 DeepSeek（最大偏差 32.3%）|

两次重复测量 token 数完全稳定，结论成立。

### 绝对锚定的真实覆盖率（新测）

- 单成分占比 >= 90% 的请求：10/18 = 56%
- 只看可用桶（排除 tiny/none）：6/9 = 67%

所以绝对锚定能覆盖约 2/3 的可用请求，值得做。但 C 方案的
「会话级调账」我原提法无效（tokMinSamples=3 在会话尺度攒不满），
正确做法是「全局基线做参照 + 会话级只记上一次值」。

### 性能（优化后 vs 优化前）

Optimised（单遍扫描 + TotalChars 精确长度）：
    纯 ASCII 42KB   78µs（原 179µs）
    纯 ASCII 1.2MB  1.16ms（原 8.7ms）
    含中文  42KB   159µs（原 141µs，略慢：TotalChars 多算一遍长度）
    含中文  1.2MB  4.0ms（原 3.4ms，略慢同理）

⚠️ 含中文路径因 TotalChars 精算全文而略慢（+13%~18%）。
这是为了「长上下文不瞎」付的代价，且绝对量小（1.2MB 才 4ms）。
纯 ASCII（最常见的代码/工具定义）快了 2~7 倍。
"""
import sys

print(__doc__)
sys.exit(0)
