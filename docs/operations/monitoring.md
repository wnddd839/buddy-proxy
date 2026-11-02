# 波动监测与告警

> 从 v0.7.0 起内置（`internal/watch`）。默认**关闭**，用 `CODEBUDDY_PROXY_WATCH=on` 开启。

## 它解决什么问题

网关「能连上」和「答得对」是两件事。最常见的两种退化都不会让 `/health` 变红：

| 现象 | HTTP | `/health` | 用户感受 |
| :--- | :--- | :--- | :--- |
| 上游不再返回推理内容（`reasoning_content` / `reasoning_tokens` 消失） | 200 | ok | 模型「不思考了」，答非所问 |
| 上游变慢 / 限频 / 换号降级 | 200 | ok | 卡、超时、质量下滑 |

外置探针只能看到自己那一发请求。内置监测看的是**全部真实流量**，不额外消耗上游额度。

## 判定方式：相对基线，不是绝对阈值

每个样本都相对**它自己的历史**比较：

```
样本 = { ok, 是否请求了推理, 推理 token 数, 耗时 }
近期窗口 = 最近 Window 个样本
基线窗口 = 近期窗口之前的 Baseline 个样本
```

三类信号：

| 信号 | 判定条件 | 说明 |
| :--- | :--- | :--- |
| 错误率 | 近期失败占比 > `ERROR_RATE` | 默认 25% |
| 推理缺失 | 近期的 `推理token=0` 占比 > `REASONING_MISS`，且比基线高 20 个百分点以上 | 只统计**客户端明确要求了推理**的请求，避免误报 |
| 耗时异常 | 近期耗时中位数 > 基线中位数 × `LATENCY_FACTOR`，且基线 ≥ 300ms | 默认 3 倍 |

样本数不足 `MIN_SAMPLES` 时**不做任何判定**（冷启动期只积累、不报警）。

## 通知节奏：绝不按次发

```
正常 ──首次越线──▶ 异常   （发 1 封 alert）
异常 ──持续──────▶ 异常   （每 REMINDER 发 1 封 remind，默认 6h）
异常 ──恢复──────▶ 正常   （发 1 封 recover）
```

一整天风平浪静 = **0 封**。降级后反复抖动也只算一次事件，直到明确恢复。

## 配置

```env
# 开关与节奏
CODEBUDDY_PROXY_WATCH=on
CODEBUDDY_PROXY_WATCH_INTERVAL=60s        # 评估周期
CODEBUDDY_PROXY_WATCH_WINDOW=120          # 近期窗口样本数
CODEBUDDY_PROXY_WATCH_BASELINE=480        # 基线窗口样本数
CODEBUDDY_PROXY_WATCH_MIN_SAMPLES=20      # 低于此样本数不判定
CODEBUDDY_PROXY_WATCH_REMINDER=6h         # 持续异常提醒间隔

# 阈值
CODEBUDDY_PROXY_WATCH_ERROR_RATE=0.25     # 也可写百分数 25
CODEBUDDY_PROXY_WATCH_REASONING_MISS=0.5
CODEBUDDY_PROXY_WATCH_LATENCY_FACTOR=3.0

# 通道一：webhook（POST JSON）
CODEBUDDY_PROXY_WATCH_WEBHOOK=https://example.com/hook
CODEBUDDY_PROXY_WATCH_WEBHOOK_TOKEN=

# 通道二：邮件（可同时启用）
CODEBUDDY_PROXY_WATCH_SMTP_ADDR=smtp.example.com:465
CODEBUDDY_PROXY_WATCH_SMTP_USER=
CODEBUDDY_PROXY_WATCH_SMTP_PASS=
CODEBUDDY_PROXY_WATCH_SMTP_FROM=alerts@example.com
CODEBUDDY_PROXY_WATCH_SMTP_TO=oncall@example.com,backup@example.com
CODEBUDDY_PROXY_WATCH_SMTP_MODE=implicit   # implicit（465）| starttls（587）| none
```

两个通道都可选，都不配就只写进程日志（`slog` warn/info）。

## 观测

### 管理台

开启后，概览页（`01 概览与监控`）指标区下方会出现一条「波动监测」：
`状态`（正常 / 异常 + 原因）、`错误率`、`推理缺失`（比例 · 缺失数/请求数）、`耗时 P50`、`样本数`。
未开启时不渲染，不会留下空位。

数据来自同一个 `watch` 状态对象，由 `/direct-admin/api/status` 携带。

### HTTP

`GET /health` 在开启后会多出一段：

```json
{
  "ok": true,
  "watch": {
    "enabled": true,
    "ok": false,
    "reason": "推理字段缺失: 87%（基线 2%）",
    "reasons": ["推理字段缺失: 87%（基线 2%）"],
    "since": 1793423688000,
    "lastAlertAt": 1793423690000,
    "metrics": {
      "samples": 120,
      "errorRate": 0.01,
      "reasoningAsked": 96,
      "reasoningMissed": 84,
      "reasoningMissRate": 0.875,
      "baselineMissRate": 0.02,
      "latencyP50": 3100,
      "baselineLatencyP50": 2950
    }
  }
}
```

`watch.ok=false` **不会**改变 `/health` 的 HTTP 状态码——网关进程本身是健康的，
把 503 抛给负载均衡只会让故障被放大。需要机器可读判据时读 `watch.ok`。

### 告警邮件长什么样

邮件正文除了指标，还带**可直接使用的排查入口**和**对号入座清单**，因为"哪个指标越线"和"根因是什么"不是一回事：

```
排查：
  1) 管理台概览页的「波动监测」条：https://<你的域名>/direct-admin/
  2) 进程日志：journalctl -u <服务名> | grep 'watch:'
  3) 换一个问法手动打一发（不要复用同一句）

对号入座：
  · 502 且上游返回 403 code 11140 → 上游把重复/相似请求判为 illegal，不是网关故障
  · 200 但没有 reasoning → 上游降档或模型默认关闭思考
  · 只有推理 token 下降、内容仍在 → 账号切换或降档，不是服务宕机
```

> 为什么强调"换一个问法"：上游会对**完全相同的请求**返回 `403 request illegal (code 11140)`。
> 用固定 prompt 做定时探活，探针自己就会被判成重复请求，报出来的"故障"是假的。

管理台地址优先取 `CODEBUDDY_PROXY_PUBLIC_BASE_URL`，没配则回落到 `http://127.0.0.1:<PORT>/direct-admin/`。

## webhook 载荷

```json
{
  "kind": "alert",
  "reason": "推理字段缺失: 87%（基线 2%）",
  "reasons": ["推理字段缺失: 87%（基线 2%）"],
  "at": 1793423690000,
  "since": 1793423688000,
  "metrics": { "...": "同上" }
}
```

`kind` 取值 `alert` / `remind` / `recover`。接收端只需按 `kind` 去重即可。

## 排查建议

- **只有推理缺失、HTTP 仍 200**：多为上游降档、账号切换或模型默认关闭思考。先确认客户端确实传了
  `reasoning_effort`；再对比换号前后。
- **错误率与耗时同时上升**：先看上游限频和管理台号池状态，其次看网络出口。
- **冷启动后一直不判定**：样本数没到 `MIN_SAMPLES`。降低该值或等流量积累。

## 已知边界

- 样本只在内存里，进程重启后基线清零，需要重新积累。
- 按进程聚合，不区分模型；多模型混跑时基线取的是整体中位数。
- 不做持久化与历史曲线，适合告警，不适合做容量分析（那是 `proxy-usage.json` 的职责）。
