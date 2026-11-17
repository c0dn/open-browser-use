## [2026-11-17 11:08] | Task: Detect Vivaldi browser profiles

### Execution Context

- **Agent ID**: `build`
- **Base Model**: Claude Opus 5.5
- **Runtime**: OpenCode on macOS

### User Query

> The extension is installed and connected in Vivaldi, but `open-browser-use profiles` reports no profiles. Add Vivaldi support.

### Changes Overview

**Scope:** CLI browser profile discovery, tests, skill docs, and release notes.

- Add a `vivaldi` browser root on macOS, Linux, and Windows.
- Profile listing, unpacked extension detection, instance-id resolution, and `--browser vivaldi` native host manifest paths now use that root.
- List `vivaldi` in the `--browser` help text and skill guidance.
- Add tests for CRX and unpacked Vivaldi installs, instance-id resolution, and the Vivaldi native host manifest path.

### Design Intent

Vivaldi is Chromium-based. It uses the same `Default` / `Profile N` layout, `Secure Preferences`, and `Local Extension Settings` as Chrome. The native host connection already worked, because the manifest is accepted from Chrome's directory. However, discovery read only the Chrome, Chrome Beta, and BitBrowser roots, so a connected Vivaldi profile was invisible. Adding the root lets the existing discovery code handle Vivaldi without a separate path.

### Validation

- `go test ./...`: passed.
- `node --test apps/chrome-extension/*.test.mjs`: passed.
- Manual run on macOS with Vivaldi and an unpacked extension: `profiles --connected` lists `vivaldi:Default` as connected.

### Files Modified

- `cmd/open-browser-use/main.go`
- `cmd/open-browser-use/main_test.go`
- `skills/open-browser-use/SKILL.md`
- `skills/open-browser-use/references/installation.md`
- `docs/releases/feature-release-notes.md`
- This history file.
