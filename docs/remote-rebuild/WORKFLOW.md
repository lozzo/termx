# TermX Remote Rebuild Workflow

Status file for unattended remote rebuild work. Update this file before starting and after completing every todo.

## Current State

- Current phase: P3 embedded local web first
- Active todo: P3-D-B shared terminal client and Terminal.tsx boundary
- Last updated: 2026-05-01T11:39:59+08:00
- Worktree goal before final response: clean after each completed todo commit

## Ordered Todos

| ID | Phase | Todo | Status | Commit |
| --- | --- | --- | --- | --- |
| R0 | workflow | Create and seed `docs/remote-rebuild/WORKFLOW.md` with full todo plan | completed | `8734d00` |
| R1 | planning | Revise remote rebuild plan so the early path builds `termx` embedded local web, shared remote UI components, and local WebRTC-over-TCP before migrating the same UI to mobile app | completed | `6a657be` |
| R2 | planning | Record that remote UI page code, architecture, and component boundaries should stay as synchronized with `../tgent` as practical while TermX message handling should emulate native app behavior where tgent interactions feel too web-like | completed | `0ce0023` |
| P2-A | identity | Implement Ed25519 machine key generation, load, persistence permissions, and fingerprint helpers in `termx-core/internal/remote/identity` | completed | `5aef5b8` |
| P2-B | cert | Implement canonical app certificate payload, sign/verify helpers, and nonce/timestamp replay helper in `termx-core/internal/remote/cert` | completed | `62d1f70` |
| P2-C | pairing | Implement local pair session creation, TTL, single-use semantics, and app certificate issuance in `termx-core/internal/remote/pairing` | completed | `12067cb` |
| P2-D | CLI | Keep `termx remote status` working and add a conservative `termx pair` CLI skeleton only after core primitives exist | completed | `4b24258` |
| P3-A | rendezvous | Implement anonymous rendezvous interfaces/contracts with payload limit, TTL, channel secret verification, and no TURN credentials | completed | `4012a1b` |
| P3-B | localweb | Implement embedded local web foundation served from `termx` binary with local status, terminal list, and pair API contracts | completed | `6d9048f` |
| P3-C | rtc | Implement local WebRTC signaling and ICE TCP mux/over-TCP support for browser-to-daemon local connections | completed | `52d964b` |
| P3-D-A | remote-ui | Create shared `remote-ui/` TypeScript package with machine/terminal contracts, transport interfaces, connection message reducer, and event queue | completed | `fe9025d` |
| P3-D-B | remote-ui | Adapt `Terminal.tsx` from `../tgent` into shared remote UI using `terminal_id` instead of pane/session concepts | in_progress |  |
| P3-D-C | remote-ui | Adapt terminal list from `../tgent` `SessionList.tsx` into `TerminalList.tsx` with machine -> terminal semantics only | pending |  |
| P3-D-D | remote-ui | Adapt `FileManager.tsx` and file hooks from `../tgent` behind TermX file transport interfaces | pending |  |
| P3-E | local/e2e | Wire embedded local web to terminal and file manager over local WebRTC DataChannels and validate in browser before mobile migration | pending |  |
| P3-F | rendezvous | Implement anonymous rendezvous HTTP adapter/service after local embedded web path is stable | pending |  |
| P4-A | mobile | Recreate mobile app shell around the shared remote UI components and replace browser adapters with native/mobile adapters | pending |  |

## TDD Log

### P3-D-B shared terminal client and Terminal.tsx boundary

- Tests written before implementation: `remote-ui/src/terminalClient.test.ts`, `remote-ui/src/useTerminalSession.test.tsx`, `remote-ui/src/Terminal.test.tsx`, and `remote-ui/src/test/mockTerminalTransport.ts`.
- Expected failing tests: `cd remote-ui && npm test` fails because `./terminalClient`, `./useTerminalSession`, and `./Terminal` do not exist yet. The tests cover `terminalId`-only public identity, `terminal:{terminal_id}` channel labels, terminal output/snapshot/info handling without pane/session/window fields, reattach preserving terminal identity, reducer-backed app resume verification, and a lightweight TermX `Terminal.tsx` component boundary with no tgent pane/session props.
- Actual failing tests before implementation: `cd remote-ui && npm test` failed as expected with missing module errors for `./terminalClient`, `./useTerminalSession`, and `./Terminal`; existing P3-D-A tests still passed.
- Planned scope: create the TermX terminal client/hook/component seam in `remote-ui/` by adapting the tgent terminal client/component boundary without copying `paneId`, `PaneInfo`, session/window/pane messages, or direct browser/native transport dependencies. Full xterm rendering polish can follow after the client/message contract is stable.
- Review regression tests: added tests for streaming terminal output before snapshot, deriving connection mode from injected transport info, and preserving close reasons as failed terminal channel state. These failed before the review fixes.
- Focused tests after implementation and review fixes: `cd remote-ui && npm test` passed 21 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm audit` passed with 0 vulnerabilities.
- Broader tests after implementation and review fixes: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Hypatia` found terminal output chunks were dropped before rendering, close reasons were overwritten by clean close handling, and shared terminal session state was hardcoded to local mode. Fixed by storing terminal output text in the hook/component boundary, deriving mode from `transport.getConnectionInfo()`, and letting reasoned close lifecycle messages preserve failed channel state.
- Result: ready to commit. Commit: pending.

### P3-D-A shared remote UI logic package

- Tests written before implementation: `remote-ui/src/model.test.ts`, `remote-ui/src/connectionMessageReducer.test.ts`, `remote-ui/src/eventQueue.test.ts`, and `remote-ui/src/transport.test.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --runInBand` should fail because `model`, `connectionMessageReducer`, `eventQueue`, and `transport` exports do not exist yet. The tests cover machine/terminal public model normalization, workspace/tab/window/pane key rejection, native-like app resume verification, offline/online reconnect intent preservation, visible error routing, event ordering/dedup/backpressure, and interface-only transport boundaries.
- Actual failing tests before implementation: `cd remote-ui && npm test` failed as expected because `./model`, `./connectionMessageReducer`, and `./eventQueue` were missing. `transport.test.ts` passed because it currently only performs compile-time interface assertions and imports the missing transport surface as type-only.
- Planned scope: create `remote-ui/` as a top-level TypeScript package, keep pure logic/adapters testable without a browser, and use tgent component/API structure only as reference while replacing `server/session/window/pane` public semantics with `machine/terminal`.
- Exploration: `Sagan` reviewed tgent component boundaries and confirmed `Terminal.tsx`, terminal client, and file client/file manager boundaries are useful references, while `SessionList.tsx` must only inform `TerminalList.tsx` structure and cannot carry session/window/pane public semantics. State for app resume, network switching, reattach, duplicate events, backpressure, and toast/banner/modal routing should live in `connectionMessageReducer`/`ConnectionEventQueue`.
- Focused tests after implementation: `cd remote-ui && npm test` passed 10 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm audit` passed with 0 vulnerabilities.
- Broader tests after implementation: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Parfit` found no high/medium issues. Low findings were stale workflow text and a top-level-only transport implementation leakage guard. Fixed by updating this workflow and adding a regression proving nested `nativePlugin` payloads are rejected by `ConnectionEventQueue`/`assertMessageBoundary`.
- Result: completed. Commit: `fe9025d`.

### R0 seed persistent workflow file

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: not run; documentation/workflow-only todo.
- Broader tests: not run; documentation/workflow-only todo.
- Result: completed. Commit: `8734d00`.

### R1 embedded local web first plan revision

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: not run; documentation/workflow-only todo.
- Broader tests: `git diff --check` passed.
- Result: completed. Commit: `6a657be`.

### R2 tgent-aligned UI and native-like message handling constraint

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: pending.
- Broader tests: `git diff --check` passed.
- Result: completed. Commit: `0ce0023`.

### P2-A machine key generation/load/fingerprint

- Tests written before implementation: `termx-core/internal/remote/identity/identity_test.go`
- Expected failing test: `go test ./internal/remote/identity` fails to build because `LoadOrCreateMachineKey`, `MachineKeyFilename`, and `MachinePublicKeyFingerprint` do not exist yet.
- Focused tests: failed as expected before implementation.
- Code review regression tests: added concurrent first-run and private-key JSON leak tests; `go test ./internal/remote/identity` fails as expected because `MachineKey.Sign` and hidden `privateKey` do not exist yet.
- Follow-up review regression tests: added formatting redaction test; `go test ./internal/remote/identity` failed as expected before `String`/`GoString` redaction.
- Final focused tests: `cd termx-core && go test ./internal/remote/identity` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `5aef5b8`.

### P2-B app certificate canonical/sign/verify/replay

- Tests written before implementation: `termx-core/internal/remote/cert/cert_test.go`
- Expected failing test: `go test ./internal/remote/cert` fails to build because `CanonicalPayload`, `AppCertificatePayload`, `SignAppCertificate`, `VerifyAppCertificate`, and `NewReplayWindow` do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: app public key length and duplicate capabilities initially accepted; `go test ./internal/remote/cert` failed before validation was tightened.
- Code review regression tests: signer initially accepted caller-supplied machine fingerprint and could issue a certificate it would later reject; `go test ./internal/remote/cert` failed before signer stamped the fingerprint from `machineKey.PublicKey`.
- Final focused tests: `cd termx-core && go test ./internal/remote/cert` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `62d1f70`.

### P2-C local pair session primitives

- Tests written before implementation: `termx-core/internal/remote/pairing/session_test.go`
- Expected failing test: `go test ./internal/remote/pairing` fails to build because `NewManager`, `Config`, `Manager`, and `ClaimRequest` do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: invalid app public key initially consumed the one-time secret; `go test ./internal/remote/pairing` failed before claim consumption moved after successful certificate issuance.
- Code review regression tests: unsupported requested capabilities initially received machine-signed certificates; `go test ./internal/remote/pairing` failed before capabilities were restricted to `terminal` and `file_manager`.
- Final focused tests: `cd termx-core && go test ./internal/remote/pairing` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `12067cb`.

### P2-D CLI pair skeleton and remote status regression

- Tests written before implementation: `termx-core/protocol/client_test.go`, `termx-core/remote_test.go`, and `termx-cli/cmd/termx/main_test.go`
- Expected failing test: `go test ./protocol -run TestClientRemotePairStart` fails to build because `PairStartParams`, `PairStartResult`, and `Client.RemotePairStart` do not exist yet.
- Additional expected failing test: `go test ./cmd/termx -run 'TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` fails to build because `pairStartClient` and the top-level `pair` command do not exist yet.
- Focused tests: failed as expected before implementation at protocol/server/CLI layers.
- Hardening regression tests: repeated pair starts with a new `--local-url` initially reused the stale URL; `go test . -run TestE2ERemotePairStartUsesLatestLocalPairURL` failed before server pairing config updates were added.
- Code review regression/fix: review found that replacing the server pairing manager on config changes could invalidate unexpired sessions. Fixed by adding `pairing.Manager.UpdateConfig`, storing session issuer config per session, and updating future-session config without discarding existing sessions.
- Final focused tests: `cd termx-core && go test ./protocol -run TestClientRemotePairStart` passed; `cd termx-core && go test . -run 'TestE2ERemote(PairStart|Status)'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` passed.
- Broader tests: `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `4b24258`.

### P3-A anonymous rendezvous interfaces/contracts

- Tests written before implementation: `termx-core/internal/remote/rendezvous/channel_test.go`
- Expected failing test: `go test ./internal/remote/rendezvous` fails to build because `NewMemoryStore`, `Config`, `CreateChannelRequest`, `Message`, and message type constants do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: unsupported message types were initially accepted; `go test ./internal/remote/rendezvous` failed before message types were restricted to offer/answer/candidate.
- Code review regression tests: excessive TTL, non-JSON or non-signaling payloads, different app public keys after claim, and unbounded per-channel messages initially passed or failed to build; `go test ./internal/remote/rendezvous` failed before max TTL, structured signaling payload validation, app public key binding, and message count limits were added.
- Final focused tests: `cd termx-core && go test ./internal/remote/rendezvous` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `4012a1b`.

### P3-B embedded local web foundation

- Tests written before implementation: `termx-core/internal/remote/localweb/handler_test.go`, `termx-core/remote_localweb_test.go`, and `termx-cli/cmd/termx/main_test.go`.
- Expected failing test: `cd termx-core && go test ./internal/remote/localweb` fails to build because `Status`, `Terminal`, `NewHandler`, `Config`, and `NewStaticAssets` do not exist yet; `cd termx-core && go test . -run TestE2ERemoteLocalWebHandlerStatusTerminalsAndPair` fails to build because `Server.LocalWebHandler`, `LocalWebOptions`, and `NewLocalWebStaticAssets` do not exist yet.
- Additional expected failing test: `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocalWebAddrFromEnv|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` fails to build because `remoteLocalWebAddrFromEnv` and `startRemoteLocalWeb` do not exist yet.
- Focused tests: failed as expected before implementation at core handler, core server wrapper, and CLI daemon local web layers.
- Review regression tests: `cd termx-core && go test ./internal/remote/localweb -run TestHandlerErrorResponseUsesDocumentedEnvelope` failed before local API errors were changed to the documented `error.code`, `error.message`, and `error.request_id` envelope.
- Final focused tests: `cd termx-core && go test ./internal/remote/localweb` passed; `cd termx-core && go test . -run 'TestE2ERemote(LocalWeb|Pair|Status)'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemote(LocalWeb|Config)|TestStartRemoteLocalWeb|TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed; `git diff --check` passed.
- Code review: `Avicenna` pre-CLI review found pair response/docs and embedded asset coverage might be incomplete from an earlier snapshot, plus `last_active_at` creation-time semantics. `Bacon` final review found stale workflow state, missing CLI listener-path coverage for terminals/pair, the same `last_active_at` placeholder, and local API error envelope drift.
- Fixes after review: local pair response/docs include `machine_public_key_fingerprint` and `expires_at`; default assets use `go:embed`; CLI TCP listener test covers `/`, `/api/local/status`, `/api/local/terminals`, and `/api/local/pair`; local API errors now use documented `code/message/request_id`; `last_active_at` remains recorded as a placeholder until runtime activity metadata exists.
- Result: completed. Commit: `6d9048f`.

### P3-C local WebRTC signaling and ICE TCP mux

- Tests written before implementation: `termx-core/internal/remote/localweb/handler_test.go`, `termx-core/internal/remote/rtc/offer_signature_test.go`, `termx-core/internal/remote/rtc/tcp_mux_test.go`, `termx-core/remote_localweb_test.go`, and `termx-cli/cmd/termx/main_test.go`.
- Expected failing tests: `cd termx-core && go test ./internal/remote/localweb -run TestHandlerLocalRTCOfferAnswersWithLocalContract` failed to build because local RTC DTOs, interface, and route did not exist; `cd termx-core && go test ./internal/remote/rtc -run 'TestOfferSignature|TestLocalICETCPMux'` failed to build because offer signature helpers and local ICE TCP mux did not exist; `cd termx-core && go test . -run TestE2ERemoteLocalWebHandlerAnswersAuthenticatedRTCOffer` failed to build because public `StartLocalICETCPMux`, `LocalWebOptions.ICETCPMux`, and signature helpers did not exist; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocalICETCPAddrFromEnv|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` failed to build because CLI ICE TCP env parsing and local web mux injection did not exist.
- Focused tests after implementation: `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test . -run 'TestE2ERemote(LocalWeb|Pair|Status)|TestE2E_WebRTC'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Broader tests before final code review: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed.
- Implementation notes: first P3-C slice uses an independent local ICE TCP listener instead of tgent-style same-port cmux; local offer verification checks machine-signed app certificate, app certificate machine_id against the local machine, app offer signature, nonce replay, requested terminal existence, and both terminal/file_manager capabilities before calling the existing WebRTC answer path.
- Deferred from this slice: browser smoke and same-port HTTP/ICE TCP cmux remain in later P3 work; current contract exposes the separate ICE TCP endpoint through local status and local RTC answer metadata.
- Scope constraints: no TermX TURN relay credentials in local responses, no workspace/tab/pane public model, app private key stays app-local, machine private key stays machine-local, and Web/native transport behavior remains behind interfaces.
- Code review findings before completion: signed local RTC offers verified `terminal_id`, but the accepted WebRTC terminal transport still exposed the full server protocol; data-channel capability checks were too broad in the first implementation; missing negative tests for wrong machine/terminal/capability/stale signature cases; workflow next action was stale.
- Next fix under TDD: add regression tests proving a local RTC offer signed for one terminal cannot snapshot/attach another terminal, wrong channel labels are denied, file-only/terminal-only certificates are rejected by the full local RTC contract, wrong local machine IDs are rejected, nonexistent terminals fail at the HTTP endpoint, and stale timestamps are rejected.
- Expected failing regression: `cd termx-core && go test . -run 'TestE2ERemoteLocalWebHandler(AnswersAuthenticatedRTCOffer|RejectsInvalidRTCOfferAuth)'` fails because a local RTC terminal data channel signed for terminal `1` can still snapshot terminal `2`.
- Fix after failing regression: added terminal-scoped transport enforcement for local RTC terminal data channels, added RTC channel policy to close unauthorized `terminal:*`, `api`, and `file:*` channels, and added negative tests for nonexistent terminals, stale offer signatures, insufficient capabilities, and app certificates signed for the wrong local machine.
- Final focused tests before review: `cd termx-core && go test . -run 'TestE2ERemoteLocalWebHandler(AnswersAuthenticatedRTCOffer|RejectsInvalidRTCOfferAuth)'` passed; `cd termx-core && go test ./internal/remote/rtc -run 'TestAnswerOfferChannelPolicyRejectsWrongTerminalChannel|TestOfferSignature|TestLocalICETCPMux'` passed.
- Final review findings: `Cicero` found one high issue where ICE TCP could be reported enabled without a usable loopback TCP candidate, plus stale workflow text. No workspace/tab/pane leakage, local TURN credentials, app machine-private-key exposure, transport-boundary leakage, or separate agent binary was found.
- Review regression: `cd termx-core && go test ./internal/remote/rtc -run TestLocalICETCPMuxAnswerIncludesLoopbackTCPCandidate` failed before the fix because the answer SDP contained TCP candidates on non-loopback interface IPs but did not contain `127.0.0.1`.
- Review fix: local ICE TCP mux now enables loopback candidate gathering and filters candidates to the bound listener IP when the listener is bound to a specific address; the answer SDP test now proves a passive loopback TCP host candidate exists.
- Final focused tests after review fix: `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test . -run 'TestE2ERemoteLocalWebHandler(AnswersAuthenticatedRTCOffer|RejectsInvalidRTCOfferAuth)|TestE2E_WebRTC'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Final broader tests after review fix: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed.
- Result: completed. Commit: `52d964b`.

## Subagents

- `Galileo` (`019ddf61-2c8a-7cf0-8a00-991250f8b294`): explorer for termx-core P2 identity/runtime integration points. Result: preserve existing `DeviceIdentity` status baseline, add machine-named key/cert/pairing primitives, and avoid exposing machine private key.
- `Lovelace` (`019ddf61-2c9d-76b2-b547-7b4afd1f5cfe`): explorer for termx-cli remote command/test structure. Result: future `termx pair` should be top-level, use protocol/public core API, and leave `termx remote status` unchanged.
- `Fermat` (`019ddf63-ea27-7260-94ef-84bb9b078503`): P2-A code review. Findings: first-run concurrency race, exported private key boundary, stale workflow. Result: fixed with exclusive install/reload, unexported private key plus signer method, and workflow updates.
- `Jason` (`019ddf6a-ef0e-7e32-89da-880e1a2590f5`): P2-A follow-up review. Findings: Go formatting could expose unexported private key fields, stale workflow. Result: fixed with redacted `String`/`GoString` and formatting regression tests.
- `Socrates` (`019ddf76-3981-7193-b7c7-2a9ba59d4941`): P2-B code review. Findings: signer could issue self-inconsistent machine fingerprint, workflow stale. Result: fixed by stamping `MachinePublicKeyFingerprint` from `machineKey.PublicKey` before canonical signing and updating workflow.
- `Dirac` (`019ddf7d-2bf6-78d0-89d0-e886099b6d01`): P2-C code review. Findings: arbitrary requested capabilities could be machine-signed, local pair request struct lacked snake_case JSON tags, workflow stale. Result: fixed with capability allowlist, JSON tags, and workflow updates.
- `Averroes` (`019ddf8c-a240-7063-8d97-e582d128011f`): P2-D code review. Findings: changing pair config replaced the manager and invalidated active sessions, workflow stale. Result: fixed with manager config updates that preserve existing sessions and workflow updates.
- `Epicurus` (`019ddf9b-6e36-7c32-9433-71c59a027d69`): P3-A code review. Findings: unbounded TTL, arbitrary data under signaling message types, missing app binding after claim, unbounded message retention, workflow stale. Result: fixed with max TTL, structured signaling payload validation, app public key binding, per-channel message limits, and workflow updates.
- R1: no subagent launched; documentation-only planning adjustment requested by the user and no implementation correctness review was required.
- `Raman` (`019de136-503d-7d10-8738-0d95fcd79bda`): explorer for P3-B core local web/server integration points. Result: use `Server.List`, `Server.RemoteStatus`, and the existing pairing manager through narrow localweb interfaces; avoid importing root `termx` from internal packages; add local status machine-key fingerprint without exposing the machine private key.
- `Avicenna` (`019de13e-d21e-76c2-98ac-f424e1250e2f`): P3-B code review before CLI daemon wiring. Findings: pair response/docs and embedded asset coverage looked incomplete from the reviewed snapshot, and terminal `last_active_at` currently maps creation time. Result: pair response/docs and embedded default assets are present in local files; CLI daemon serving path and `last_active_at` placeholder documentation remain active work inside P3-B.
- `Bacon` (`019de14b-634e-7e33-b3e8-231a41bd965a`): final P3-B code review after CLI daemon wiring. Findings: workflow stale, CLI listener test did not cover terminals/pair, `last_active_at` is creation time, and local error responses lacked `code/request_id`. Result: workflow updated, CLI listener coverage expanded, error envelope fixed; `last_active_at` remains a documented placeholder.
- `Nash` (`019de156-1151-77b3-ad01-5b2e67f253e0`): P3-C explorer for local RTC handler/contract boundaries. Result: keep `localweb` behind a narrow `RTCOfferAnswerer` interface, verify app certificate/signature/nonce/requested terminal before answering, reuse `rtc.AnswerOffer`, and expose `session_id`, `machine_id`, and `terminal_id` in local offer docs.
- `Linnaeus` (`019de156-272a-7541-8ef0-20fe88d2043f`): P3-C explorer for local WebRTC-over-TCP slice. Result: first slice should add an independent local ICE TCP listener and status metadata instead of same-port cmux; CLI envs are `TERMX_REMOTE_LOCAL_ICE_TCP_ENABLE` and `TERMX_REMOTE_LOCAL_ICE_TCP_ADDR`.
- `Beauvoir` (`019de165-691d-7d80-86da-dbd140400107`): P3-C code review before commit. Findings: signed local RTC offers did not scope the terminal protocol after data-channel acceptance; capability gating was too coarse; machine identity comparison and negative security tests needed explicit coverage; workflow next action was stale. Result: fixed with terminal-scoped transport, channel policy, machine identity checks, and negative tests.
- `Cicero` (`019de177-2141-7570-b3c6-30248c4bd521`): P3-C final code review. Findings: ICE TCP could be reported enabled without a usable loopback TCP candidate; workflow stale. Result: fixed with loopback candidate gathering, bound-listener IP filtering, answer SDP regression test, and workflow updates.
- `Sagan` (`019de189-868e-71c1-8010-25433d7758e6`): P3-D read-only explorer for tgent UI boundaries. Findings: keep `Terminal.tsx`, terminal client, file client, and file manager boundaries close where practical; convert `paneId` to `terminalId`; treat `SessionList.tsx` as structure-only input for `TerminalList.tsx`; move lifecycle, reattach, duplicate event handling, backpressure, and visible error routing to TermX reducer/queue.
- `Parfit` (`019de18f-90ad-70e2-998b-2ea5182f0390`): P3-D-A code review. Findings: stale workflow text and nested transport implementation leakage could pass the first boundary guard. Result: workflow updated and nested `nativePlugin`/browser transport leakage now fails under `ConnectionEventQueue` tests.
- `Hypatia` (`019de19b-9371-7c21-8fe9-988106a95441`): P3-D-B code review. Findings: output chunks were dropped, reasoned terminal closes were overwritten to clean closed state, and mode was hardcoded to local. Result: fixed with output text state, transport-derived mode, failed close preservation, and regression tests.

## Code Review Log

- P2-A review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-B review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-C review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-D review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, CLI internal-package import, or transport boundary drift introduced.
- P3-A review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay credentials, terminal/file data relay, app machine-private-key exposure, or transport boundary drift introduced.
- R1 planning review: self-checked updated docs for scope drift. Plan now prioritizes embedded local web, shared `remote-ui/` components, and local WebRTC-over-TCP before mobile app migration; no new workspace/tab/pane public model and no anonymous/free TURN relay entitlement were introduced.
- R2 planning review: self-checked updated docs for scope drift. Plan now requires tgent-aligned page/component structure where practical, plus TermX-owned native-like message reducer/event queue behavior; no new workspace/tab/pane public model and no anonymous/free TURN relay entitlement were introduced.
- P3-B review complete. No remaining blocker after implemented fixes; no workspace/tab/pane public model, anonymous/free TURN relay credentials, app machine-private-key exposure, or transport boundary leakage introduced. Residual risk is limited to the documented `last_active_at` placeholder and the P3-C absence of local RTC/ICE TCP data plane.
- P3-C review complete. No remaining blocker after implemented fixes; no workspace/tab/pane public model, anonymous/free/local TURN relay credentials, app machine-private-key exposure, transport boundary leakage, or separate agent binary was introduced. Residual risk: browser smoke with the embedded web UI is still deferred to P3-E because P3-C only proves local signaling, terminal protocol scoping, file/API channels, and server-side ICE TCP candidate generation.
- P3-D-A review complete. No remaining blocker after implemented fixes; no workspace/tab/pane/window/session public model was introduced in `remote-ui`, signaling `sessionId` remains limited to local RTC interfaces, no anonymous/free/local TURN relay credentials or machine private key exposure were introduced, and business logic remains behind transport interfaces without direct browser/native implementation imports. Residual risk: React components, browser local adapter, terminal/file UI behavior, and browser smoke remain deferred to later P3-D/P3-E todos.
- P3-D-B review complete. No remaining blocker after implemented fixes; `Terminal.tsx`, `TerminalClient`, and `useTerminalSession` expose `machineId`/`terminalId` and `terminal:{terminal_id}` only, do not import browser/native transport implementations, do not expose machine private key material, and do not introduce TURN relay credentials. Residual risk: this is a minimal terminal boundary with plain text rendering; full xterm rendering, browser local adapter wiring, and browser smoke remain deferred to later P3-D/P3-E todos.

## Deferred Human Decisions And Placeholders

- Public rendezvous deployment, DNS, TLS certificates, billing/subscription provider, mobile signing, and app store configuration remain deferred by policy.
- `termx-core/remote_localweb.go` currently maps `last_active_at` from terminal creation time because the existing terminal inventory does not expose a separate last-activity timestamp. This is a narrow placeholder; replace it with real activity metadata when the terminal runtime publishes it.
- Local HTTP and ICE TCP currently use independent listeners. Same-port cmux remains deferred until browser smoke/local e2e proves that reducing exposed ports is worth the extra listener complexity.

## Risks

- Existing baseline uses `DeviceID` terminology while remote rebuild docs require public `machine -> terminal` object language. The implementation should preserve compatibility where needed but introduce machine-key/certificate concepts without exposing workspace/tab/pane.
- Existing hub baseline may include relay fields. P3 anonymous paths must explicitly reject or omit TermX TURN relay credentials.
- New `remote-ui/` package must avoid carrying over tgent pane/session public concepts when copying `Terminal.tsx`, `SessionList.tsx`, and file manager code.
- Keeping TermX UI close enough to tgent for future synchronization conflicts with replacing tgent's web-like interaction state. The boundary is explicit: copy structure/components/adapters where possible, but normalize messages and lifecycle through TermX reducers/queues.

## Next Exact Action

1. Commit P3-D-B with the shared terminal client/hook/component boundary and workflow updates.
2. Record the P3-D-B commit hash in this workflow.
3. Start P3-D-C by writing failing tests for `TerminalList.tsx` with machine -> terminal semantics only and no tgent session/window/pane model.
