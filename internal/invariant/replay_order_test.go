package invariant

import (
	"testing"
	"time"

	"github.com/wnddd839/codebuddy-proxy/internal/decision"
)

// 回放必须先把**所有** quota 状态就位，再跑 request 记录。
//
// ## 缺陷（2026-11-24 回放预演时发现）
//
// 同一个请求的两个记录，时间戳口径不同：
//
//	request 的 At = 请求**开始**时刻（input.Started）
//	quota deduct 的 At = 请求**结束**时（MarkResult 调用时）
//
// 实测 deduct_at - request_at：最小 402ms、中位 3323ms、最大 63.6s，
// 812 条**全部**是 deduct 晚于 request（0 条相反）。
//
// 而 Replay 按 At 升序逐条跑，于是 request 永远先到、deduct 后到：
//
//	request 到达 → TakeDeduct 找不到记录 → 报 I2a「没有对应的扣减记录」
//	deduct 到达 → PrimeFromRecord 存进去，但已无人消费
//
// 结果：回放对每一条正常扣费都报一次 I2a，1942 次全是假象。
// **线上是正确的**（MarkResult 发生在 recordChatJournal 之前）。
//
// ## 修法
//
// 两阶段：① 先扫全部 quota 记录建状态（只 PrimeFromRecord，不判）；
// ② 再扫 request/selection 记录跑断言。quota 自身的断言（I1/I2b）
// 在阶段②仍然要跑，所以阶段①要保留它们的位置 —— 用一个标记跳过
// 阶段①的 finding 产出即可（代价：quota 类别断言只判一次，位置在阶段②）。
func TestReplayPrimesQuotaBeforeRequests(t *testing.T) {
	store, err := decision.OpenStore(t.TempDir(), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	base := now.Add(-time.Hour).UnixMilli()

	// request 先写（模拟 request.At = 开始时刻）
	if err := store.Append(decision.Record{
		Kind: decision.KindRequest, At: base, Ok: true,
		RequestID: "r1", AccountID: "a1", Model: "m",
		Credit: func() *float64 { v := 1.3; return &v }(),
	}); err != nil {
		t.Fatal(err)
	}
	// deduct 后写（模拟 quota.At = 结束时刻，晚 3 秒）
	if err := store.Append(decision.Record{
		Kind: decision.KindQuota, At: base + 3000,
		AccountID: "a1", RequestID: "r1", Op: decision.OpDeduct,
		Before: func() *float64 { v := 100.0; return &v }(),
		After:  func() *float64 { v := 98.7; return &v }(),
		Credit: func() *float64 { v := 1.3; return &v }(),
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Replay(store, now.Add(-2*time.Hour), now, nil)
	if err != nil {
		t.Fatal(err)
	}

	// I2a 不该命中：扣减记录存在，只是写入时间晚于点
	for _, g := range report.Groups {
		if g.Invariant == "I2a" {
			t.Fatalf("正常扣费不该报 I2a（回放顺序问题）：%+v", g)
		}
	}
	if report.Scanned != 2 {
		t.Fatalf("应扫描 2 条，实际 %d", report.Scanned)
	}
}

// 对照：真的没有扣减记录时，I2a 仍要报（修回放顺序不能把真缺陷放过）。
func TestReplayStillCatchesMissingDeduct(t *testing.T) {
	store, err := decision.OpenStore(t.TempDir(), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	if err := store.Append(decision.Record{
		Kind: decision.KindRequest, At: now.Add(-time.Hour).UnixMilli(), Ok: true,
		RequestID: "r-missing", AccountID: "a1", Model: "m",
		Credit: func() *float64 { v := 1.3; return &v }(),
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Replay(store, now.Add(-2*time.Hour), now, nil)
	if err != nil {
		t.Fatal(err)
	}

	hit := false
	for _, g := range report.Groups {
		if g.Invariant == "I2a" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("真缺扣减记录仍应报 I2a：%+v", report.Groups)
	}
}

// quota 类别断言（I1 清空无理由）在阶段②仍要判。
func TestReplayStillRunsQuotaChecks(t *testing.T) {
	store, err := decision.OpenStore(t.TempDir(), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	// 无理由清空额度 —— I1 应报
	if err := store.Append(decision.Record{
		Kind: decision.KindQuota, At: now.Add(-time.Hour).UnixMilli(),
		AccountID: "a1", Op: decision.OpClear,
		Before: func() *float64 { v := 5.0; return &v }(),
		After:  nil,
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Replay(store, now.Add(-2*time.Hour), now, nil)
	if err != nil {
		t.Fatal(err)
	}

	hit := false
	for _, g := range report.Groups {
		if g.Invariant == "I1" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("无理由清空额度应命中 I1：%+v", report.Groups)
	}
}
