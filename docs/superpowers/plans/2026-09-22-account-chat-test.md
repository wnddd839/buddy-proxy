# Account Chat Test Implementation Plan

> **For agentic workers:** Execute task-by-task with TDD. Steps use checkbox syntax.

**Goal:** Add admin single/batch account chat tests that pin one account, measure latency, default to lowest credit-multiplier model.

**Architecture:** `models.LowestMultiplierID` + `gateway` chattest helpers (reuse `chatOptionsFromAccount`, call `Provider.Complete` with no pool side effects) + admin routes/UI mirroring checkin.

**Tech Stack:** Go 1.26, stdlib `net/http`, existing admin HTML/JS.

---

### Task 1: LowestMultiplierID

**Files:**
- Create: `internal/models/lowest.go`
- Test: `internal/models/lowest_test.go`

- [ ] Write failing tests for empty list, free(0) wins, missing multipliers fall back to first, stable tie-break
- [ ] Implement `LowestMultiplierID([]Model) (string, bool)`
- [ ] `go test ./internal/models/ -run Lowest -count=1`

### Task 2: gateway chattest

**Files:**
- Create: `internal/gateway/chattest.go`
- Test: `internal/gateway/chattest_test.go`

- [ ] Failing tests: success result shape; error message truncated; batch summary; skipped on deadline; no MarkResult on failure
- [ ] Implement `TestAccountChat` + `RunPoolChatTest` + model resolve helper
- [ ] `go test ./internal/gateway/ -run ChatTest -count=1`

### Task 3: server routes

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

- [ ] Route `POST .../test` before accounts prefix; `.../accounts/{id}/test` in handleAccountAction
- [ ] Tests for both endpoints + route conflict

### Task 4: admin UI + docs

**Files:**
- Modify: `internal/admin/page.go`
- Modify: `docs/api/http.md`

- [ ] Shared model select + 批量测试 + per-account 测试
- [ ] Document new routes in http.md
- [ ] `make check`
