# Agent Notes

## Deployment Targets

- Hub and Web Controller are deployed on `ssh root@114.66.58.243`.
- TermX runtime/agent side is deployed on `ssh al`.
- `remote-ui` is developed and tested locally from this repository.

## Deployment Workflow Reminder

- Do not assume a code change is deployed just because it is committed locally.
- After changing Hub or Web Controller code, SSH to `root@114.66.58.243`, update the checkout/artifacts there, restart the relevant service, then verify health.
- After changing TermX runtime/agent/CLI code, SSH to `al`, update the checkout/artifacts there, restart or relaunch the relevant TermX process, then verify the runtime path.
- After changing `remote-ui`, validate locally with `cd remote-ui && npm run typecheck && npm run test`; use the local dev server for browser verification.
- Before restarting remote services, inspect the existing deploy path and service names on the target host instead of guessing.

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
