# Codex Unattended Remote Rebuild Prompt

Use this prompt for the next Codex run when implementing the TermX remote rebuild.

```text
You are Codex working in /Users/lozzow/Documents/workdir/termx.

You are implementing the TermX remote rebuild. Read these documents first and treat them as the source of truth:

- docs/remote-rebuild/README.md
- docs/remote-rebuild/architecture.md
- docs/remote-rebuild/connection-modes.md
- docs/remote-rebuild/auth-and-pairing.md
- docs/remote-rebuild/api.md
- docs/remote-rebuild/mobile-app-pages.md
- docs/remote-rebuild/implementation-plan.md
- AGENTS.md
- termx-core/AGENTS.md when touching termx-core
- termx-cli/AGENTS.md when touching termx-cli
- tuiv2/AGENTS.md when touching tuiv2

Core product decisions:

- Remote public object model is machine -> terminal.
- Do not introduce workspace/tab/pane as remote public concepts.
- termx daemon contains the remote agent runtime. Do not create a separate termx-agent binary.
- Unauthenticated users can pair a machine and attempt anonymous P2P using public STUN and lightweight rendezvous signaling.
- If P2P succeeds, terminal and file manager are available because data does not traverse TermX relay.
- TermX TURN relay is a paid/subscription capability.
- Anonymous/free users must not receive TermX TURN relay credentials.
- App holds an app keypair. Machine holds a machine keypair. Agent signs app certificates. App must never download, decrypt, or hold machine private key.
- Network transport must be abstracted behind interfaces. Web and native implementations must not leak into UI/business components.

Operational mode:

- Work fully unattended. Do not stop to ask what to do next unless there is a true hard blocker that cannot be reasonably mocked or deferred.
- If something needs human input, skip it for now by adding a mock, explicit TODO, or narrow placeholder. Record it in the workflow file with why it was deferred and what exact human input is needed.
- Prefer conservative assumptions that keep the project moving and align with the remote-rebuild docs.
- Never drift into unrelated refactors.
- Never reintroduce old tgent workspace/tab/pane assumptions.

Persistent workflow file:

- Create and maintain docs/remote-rebuild/WORKFLOW.md.
- This file is mandatory and must be updated before and after every todo.
- Its purpose is to survive context loss, machine reboot, or task interruption.
- It must include:
  - Current phase and active todo.
  - Ordered todo list with statuses.
  - Commit hash for every completed todo.
  - Tests written before implementation.
  - Tests run and results.
  - Subagents launched and their scopes/results.
  - Deferred human decisions and mocks/placeholders.
  - Risks and next exact action.

Todo and commit discipline:

- Break work into small todos matching docs/remote-rebuild/implementation-plan.md.
- Use TDD for each todo:
  1. Write or update failing tests first.
  2. Run the focused tests and confirm they fail for the expected reason.
  3. Implement the smallest change.
  4. Run focused tests.
  5. Run broader relevant tests.
  6. Run a subagent code review focused on scope drift and correctness.
  7. Fix issues or record justified deferrals.
  8. Update docs/remote-rebuild/WORKFLOW.md.
  9. Commit the todo.
- Commit after every completed todo.
- Commit messages must be detailed and specific:
  - Explain motivation.
  - Describe scope.
  - Mention key implementation details.
  - Mention behavior changes.
  - Mention tests.
- After each commit, report which todo was completed and the commit hash.

Subagent usage:

- If multiple tasks are independent and can run in parallel, start multiple subagents.
- Use worker subagents for bounded implementation tasks with disjoint file ownership.
- Use explorer subagents for narrow codebase questions.
- After every development todo, start a code review subagent.
- Code review subagent prompt must emphasize:
  - Check whether the implementation deviates from docs/remote-rebuild.
  - Check for accidental workspace/tab/pane remote concepts.
  - Check that anonymous/free flow does not receive TermX TURN relay credentials.
  - Check that app never receives machine private key.
  - Check that transport boundaries remain interface-based.
  - Check tests cover the intended behavior.
  - Return findings ordered by severity with file/line references.
- Do not wait idly. While subagents run, continue non-overlapping work.

TDD expectations:

- For Go code, write unit tests close to the package being changed.
- For CLI behavior, add command tests where practical.
- For web/mobile TypeScript, add tests for pure logic, adapters, state machines, and API clients before UI polish.
- For UI, use mocked transport first, then add smoke/e2e only when the app shell exists.
- Each feature must have tests proving the product rule it implements.

Initial implementation direction:

Start with implementation-plan P2/P3 rather than app UI:

1. Create docs/remote-rebuild/WORKFLOW.md and seed it with the full todo plan.
2. Implement machine key and app certificate primitives in termx-core:
   - Ed25519 machine key generation/load/fingerprint.
   - Canonical app certificate payload.
   - Sign/verify helpers.
   - Nonce/timestamp replay helper.
   - Tests first.
3. Implement local pairing session primitives:
   - Pair session creation.
   - Secret TTL.
   - Single-use semantics.
   - Certificate issuance from app public key.
   - Tests first.
4. Implement CLI skeleton:
   - termx remote status should remain working.
   - Add or plan termx pair command only when core primitives exist.
5. Implement anonymous rendezvous contract next:
   - Start with interfaces and tests.
   - Ensure payload limits, TTL, channel secret verification.
   - Ensure no TURN relay credentials in anonymous flow.

Deferral policy:

- If public server deployment, DNS, certificates, billing provider, mobile signing, or store-specific configuration is required, use a placeholder/mock and record it in WORKFLOW.md.
- If a choice is unclear but not security-critical, choose the simplest documented option and record the assumption.
- If a choice affects key security, private key storage, or paid relay authorization, implement the safer conservative path or defer with an explicit TODO and tests around current behavior.

Before final response:

- Ensure no long-running commands are left.
- Ensure git status is clean, unless a hard blocker is explicitly recorded.
- Summarize completed todos and commit hashes.
- Summarize tests run.
- Summarize deferred human decisions.
```
