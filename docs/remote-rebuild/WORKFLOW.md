# Remote Rebuild Workflow

## Slice 10-C: Hub-Mediated App Pairing Claim

### Goal

Implement the APP pairing path where Web Control supplies account machines and Hub location only; the Hub relays pairing claims/results; the termx agent validates the pair session/secret locally and signs the app certificate.

### Tests

- Frontend regression for Web Control machine matching and Hub `POST /api/v1/pairing/claims`.
- Hub registry/httpapi round-trip for app claim, agent poll, agent result, app certificate response.
- Core runtime regression for polling Hub pairing claims, invoking the local pairing manager, and retrying a pending pairing result after a transient submit failure.
- CLI/core localweb tests updated for the APP/localweb entry split.

### Implementation Notes

- `remote-ui` Pair Device now uses the selected cloud machine's `hub_http_url` instead of QR `local_pairing`.
- `termx-hub` stores only short-lived in-memory pairing claim/result state and has no durable ownership database.
- `termx-core` injects the local pairing manager into the remote runtime so pair secret validation and certificate signing stay on the machine.
- `termx-cli` and core pairing session creation tolerate empty `LocalPairURL` for hub-mediated pairing.

### Validation

- `npx vitest run src/WebControlRemoteApp.test.tsx src/managedHubApi.test.ts src/localAppIdentity.test.ts src/localAgentApi.test.ts --reporter verbose`
- `npm run typecheck`
- `npm test`
- `npm run build`
- `npm run build:localweb`
- `go test ./internal/registry ./internal/httpapi` in `termx-hub`
- `go test ./internal/remote/localweb ./internal/remote/runtime ./internal/remote/pairing ./internal/remote/discovery ./remote/hubv1` in `termx-core`
- `go test ./cmd/termx` in `termx-cli`

### Review

Self-review found and fixed a pending pairing result retry gap: after an agent locally consumed a pair session, a transient Hub result submission failure could otherwise prevent the signed result from reaching the app. The runtime loop now retries pending results before polling new claims.

### Remaining Risk

Hub claim ownership proof is still not durable in Hub. Production should bind app-facing pairing claims to Web Control authorization or a short-lived Web Control-issued claim ticket while keeping Hub stateless.
