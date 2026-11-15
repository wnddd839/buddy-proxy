package gateway

import (
	"testing"

	"github.com/wnddd839/codebuddy-proxy/internal/config"
)

// TestKeepSystemForUsesSnapshotMode 锁住「模式快照贯穿递归」：
// opts.systemMode 一旦在首次进入时定下，中途管理员切到 fold 不应改变
// 本次请求及其递归重试实际发出的形态，也不应改变降级判定用的 mode。
func TestKeepSystemForUsesSnapshotMode(t *testing.T) {
	svc := New(config.Config{Site: "domestic", SystemPromptMode: config.SystemPromptModeAuto}, nil)
	t.Cleanup(func() { _ = svc.Close() })

	// 模拟请求进行中管理员把实时模式切成 fold。
	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeFold})
	if got := svc.systemPromptMode(); got != config.SystemPromptModeFold {
		t.Fatalf("前置条件：实时模式应为 fold，实际 %q", got)
	}

	// 首次进入时快照的是 auto（CompleteFromPool 入口会填 opts.systemMode）。
	opts := CompleteOptions{systemMode: config.SystemPromptModeAuto}
	if !svc.keepSystemFor(opts) {
		t.Fatal("快照为 auto 时应原样透传 system，不能受实时 fold 影响")
	}

	// 已降级过的重试仍强制 fold（与快照无关）。
	opts.FoldedRetry = true
	if svc.keepSystemFor(opts) {
		t.Fatal("FoldedRetry 必须强制折叠")
	}

	// fold 快照即使实时切回 auto，也仍然折叠。
	svc.SetBehaviorSwitches(BehaviorSwitches{SystemPromptMode: config.SystemPromptModeAuto})
	opts = CompleteOptions{systemMode: config.SystemPromptModeFold}
	if svc.keepSystemFor(opts) {
		t.Fatal("快照为 fold 时应始终折叠，不能受实时 auto 影响")
	}
}
