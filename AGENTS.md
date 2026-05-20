# Agent Notes

## Deployment Targets

- Hub and Web Controller are deployed on `ssh root@114.66.58.243`.
- TermX runtime/agent/CLI is developed and tested locally from this repository.
- `remote-ui` is developed and tested locally from this repository.

## Deployment Workflow Reminder

- Do not assume a code change is deployed just because it is committed locally.
- After changing Hub or Web Controller code, SSH to `root@114.66.58.243`, update the checkout/artifacts there, restart the relevant service, then verify health.
- Do not deploy TermX runtime/agent/CLI changes to `ssh al` during normal agent work.
- After changing TermX runtime/agent/CLI code and finishing local builds/tests, kill any local TermX daemon or test daemon process. Do not restart it; the user will restart it manually.
- After changing `remote-ui`, validate locally with `cd remote-ui && npm run typecheck && npm run test`; use the local dev server for browser verification.
- Before restarting remote services, inspect the existing deploy path and service names on the target host instead of guessing.

## Terminal History Direction

- Terminal history semantics should stay close to tmux: UI, copy mode, and remote terminal history read parsed grid/cell rows owned by core, not raw PTY bytes.
- Special TermX-only terminal history behavior needs a clear reason and regression coverage.
- The real PTY size is a single global terminal state. Panels, floating windows, mobile App, and `remote-ui` are observer viewports over that terminal.
- If an observer viewport differs from the PTY size, use projection only: crop, scroll window, overflow arrows, and blank-dot hints. Do not reinterpret normal terminal/copy-mode/history rows at observer width.
- Raw PTY journals are not the UI history query path. They may only return later as optional debug/audit data.
- TUI validation for terminal history changes should use real tmux-driven terminal operations when practical, not only code-level unit tests.
- Current autonomous development for this line is driven by repository-root `workflow.md`, not by old files under `docs/workflows/`.
- Old workflow files under `docs/workflows/` are historical archives only. Do not use them as the active driver unless the root `workflow.md` explicitly says so.

## Current Development Scope

- Current branch line scope: **core + tuiv2 only**.
- Active implementation surface:
  - `termx-core/`
  - `termx-vterm/`
  - `tuiv2/`
  - directly related `internal/protocol/`, `termx-proto/`, and `termx-cli/` glue only when required by core/tui contracts
- Explicitly out of scope for this phase unless the root `workflow.md` reopens them:
  - `remote-ui/`
  - `termx-app/`
  - `web-control/`
  - broader remote product integration beyond required contract hygiene

## Workflow Discipline

- Repository-root `workflow.md` is both the active driver and the running log for this effort.
- After finishing a module-sized slice, update `workflow.md` immediately.
- Keep `workflow.md` compressed:
  - preserve only current goals, current design decisions, active risks, latest validation evidence, and the next concrete steps;
  - collapse older completed slices into short summaries instead of appending long chronological transcripts forever.
- Do not let `workflow.md` grow into a replay log that overwhelms prompt context.
- If a historical record is still worth keeping in full, move it under `docs/workflows/archive/` and keep only a short pointer in `workflow.md`.

## Android Build Default

- Unless explicitly requested otherwise, build Android APKs as `debug` packages for `arm64-v8a` only, to keep artifact size smaller and download/install faster.

## App-Agent Network Boundary

- All application-to-agent data traffic must go through the established WebRTC transport.
- Do not add direct HTTP, WebSocket, TCP, localhost, LAN, or filesystem-serving shortcuts between the app/browser and the agent for terminal, file, preview, upload, download, or runtime data.
- Browser adapters such as service workers may translate browser APIs into app-local requests only when the bytes still come from WebRTC data channels.

## Development Compatibility Policy

- This repository is still in active development. Do not preserve compatibility aliases, deprecated exports, wrapper files, or old module names when refactoring.
- Prefer direct breaking changes with all call sites updated in the same change.
- If a name or boundary is wrong, rename it and fix the imports instead of adding a compatibility layer.

## Commit Message Policy

- Future git commit messages created during agent work in this repository should use Chinese.
