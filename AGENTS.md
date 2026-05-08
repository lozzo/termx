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
