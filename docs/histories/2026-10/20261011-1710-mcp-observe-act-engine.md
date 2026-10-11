## [2026-10-11 17:10] | Task: MCP observe / act fast page engine

### 🤖 Execution Context

- **Agent ID**: `build`
- **Base Model**: `Claude Opus 5.5`
- **Runtime**: `OpenCode`

### 📥 User Query

> Bring the fast observe/act loop (from Wayfarer, Maya and browser-use
> jev-ultrafast) to Open Browser Use, as a cheap experiment.

### 🛠 Changes Overview

**Scope:** Go CLI MCP server, new `internal/engine` package, skill and architecture docs

**Key Actions:**

- **[Engine]**: `internal/engine` observes a tab as numbered controls via the
  ported `snapshot.js` in an isolated world, and acts with `Input.*` after
  re-checking each control's identity, visibility and occlusion (`target.js`).
- **[MCP]**: new `observe` and `act` tools over the existing `executeCdp`
  relay; one engine page per tab keeps the last observation between calls.
- **[Background tabs]**: `Emulation.setFocusEmulationEnabled` once per
  document. Without it a hidden tab holds `mouseMoved` acknowledgements for
  about 5 s, throttles timers to 1 s and never runs animation frames.
- **[Policy]**: `internal/policy` and MCP `run_task` port Maya's Jev policy:
  one System One request per step (operation plus a target head per
  operation, `FILL_FORM` batches with per-field yes/no questions), field values
  from a small chat model, stops for passwords, one-time codes, possible
  spending, three unchanged steps and an action cap. Gateway, key and models
  come from `OBU_POLICY_*` environment variables.
- **[Errors]**: a debugger detached by another extension's frame (seen with a
  password manager's inline menu on a focused field) is reported at once as a
  `DetachedError` instead of being retried as a navigating page.

### 🧠 Design Intent (Why)

Agents spend most of their browser time reading pages and guessing selectors.
A compact outline of numbered controls plus checked, real input lets a model
(or a fast policy such as TypeSafe Jev) act in one call per step. The engine
needs only `executeCdp`, so the extension is unchanged. Cross-origin iframes,
dialogs and popups need CDP events and child sessions, which the relay does
not expose to MCP clients yet; they are left out rather than half-supported.

### 📁 Files Modified

- `internal/engine/engine.go`, `observe.go`, `act.go`, `outline.go`, `engine_test.go`
- `internal/engine/js/*`
- `internal/policy/client.go`, `policy.go`, `task.go`, `policy_test.go`
- `cmd/open-browser-use/engine.go`, `mcp.go`, `mcp_test.go`
- `skills/open-browser-use/SKILL.md`
- `docs/ARCHITECTURE.md`

### ✅ Validation

- `go vet ./...` and `go test ./...`: passed.
- Real Chrome (extension 0.1.42) through `obu mcp`: local form page (type,
  select, check, submit, covered-button refusal, same-origin iframe click,
  link and back) and a live Wikipedia search. `observe` takes 3-50 ms;
  a four-step form `act` about 1.8 s.
- `run_task` live through a System One gateway: Wikipedia search and open the
  article in 3 decisions (about 6 s), the local order form filled, checked and
  submitted in one batch (2 decisions, about 4 s).
