## [2026-10-11 23:51] | Task: run_task stops when the goal is done

### 🤖 Execution Context

- **Agent ID**: `build`
- **Base Model**: `Claude Opus 5.5`
- **Runtime**: `OpenCode`

### 📥 User Query

> In a live test Jev kept clicking until the 3-action limit and returned
> `limit` instead of `done`. Fix it.

### 🛠 Changes Overview

**Scope:** `internal/policy`

**Key Actions:**

- **[Loop]**: `Run` used to end as soon as the last allowed action was taken,
  so Jev never got to look at the result and report `DONE`. It now asks one
  more decision after the last action: `DONE`, `BLOCKED` or `NEEDS_OWNER`
  end the task as usual, and any further action stops with `limit` without
  running.
- **[Prompt]**: the next-action rule and the `DONE` description now tell Jev to
  choose `DONE` as soon as the current page shows every requirement, instead
  of clicking, re-submitting or checking again.
- **[Tests]**: `TestRunReportsDoneAfterTheLastAllowedAction` covers both
  sides of the limit.

### 🧠 Design Intent (Why)

A goal reached by the last allowed action was always reported as `limit`. The
prompt change is meant to cut the extra clicks after the goal is met. That part
depends on the model, so only a live run can show whether it works.

### 📁 Files Modified

- `internal/policy/task.go`, `policy.go`, `policy_test.go`

### ✅ Validation

- `go vet ./...` and `go test ./...`: passed. The new test fails if the loop
  change is reverted.
- Not yet re-run live against the System One gateway.
