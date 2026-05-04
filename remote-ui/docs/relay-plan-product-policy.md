# Relay Plan Product Policy

## Purpose

This document records the product and architecture decisions for TermX public P2P, managed relay, relay quota, and paid plan behavior.

The key product principle is:

- Free direct connectivity should cost TermX almost nothing.
- Paid value should come from connection reliability, managed relay availability, and relay traffic quota.
- Paid tiers should not create new client transport types.
- Runtime remains WebRTC DataChannel. Charging and limits are capability, policy, quota, and relay-session behavior.

## Connection Modes And Cost Model

Client-visible paths remain:

- `local`: local machine, LAN, or user-managed exposure such as FRP/public port. This does not use TermX cloud relay and should not count against relay quota.
- `public_p2p`: TermX rendezvous/signaling plus public or TermX-provided STUN for hole punching. If P2P succeeds, runtime traffic is direct and should not count against relay quota.
- `managed`: TermX-managed Hub/ICE/TURN infrastructure. If direct ICE fails or policy chooses relay, runtime traffic may use TermX relay and should count against relay quota.

Do not introduce `relay`, `basic`, `pro`, `paid_relay`, or similar values as new transport/path taxonomy.

## Product Tiers

### Unregistered / Offline Free

Allowed:

- Local connection.
- LAN connection.
- User-managed exposure such as FRP or a public port.

Not allowed:

- TermX cloud rendezvous.
- TermX managed relay.

Rationale: this path can be fully private and does not consume TermX cloud resources.

### Registered Free

Allowed:

- TermX rendezvous/signaling.
- Public STUN hole punching.
- Full runtime features if P2P succeeds: terminal, file, API, and events.

Not allowed:

- TermX relay when P2P fails.

Rationale: signaling cost is low and useful for acquisition. Relay traffic has real bandwidth cost and should be paid.

### Paid Relay Plans

Allowed:

- TermX rendezvous/signaling.
- STUN-assisted P2P.
- TermX managed relay when P2P fails or relay is selected by Hub/ICE policy.
- Runtime features remain available over relay, subject to quota and throttling.

Plan differences should primarily be:

- Monthly relay traffic quota.
- Maximum simultaneous relay sessions.
- Over-quota throttling state.

Plan differences should not require different runtime APIs, different DataChannel protocols, or different client transport types.

Example initial shape:

| Plan | Monthly relay quota | Simultaneous relay sessions | In-quota speed | Over-quota behavior |
| --- | ---: | ---: | --- | --- |
| Basic | 1 GB | 1 | Normal shared relay service | Throttle to terminal-friendly low speed |
| Pro | 5 GB | 2 | Normal shared relay service | Throttle to terminal-friendly low speed |
| Max | 20 GB or more | 5 | Normal shared relay service | Throttle to terminal-friendly low speed |

Exact numbers should be revisited after bandwidth cost modeling.

## Quota And Throttle Rules

Relay traffic accounting:

- Count only traffic that actually uses TermX relay/TURN.
- Do not count local/LAN/FRP traffic.
- Do not count successful direct public P2P runtime traffic.
- Count both upload and download relay bytes unless future pricing explicitly separates them.

Behavior before quota is exhausted:

- Do not throttle per paid tier.
- Do not add higher-priority lanes by plan in the first version.
- Keep runtime behavior identical across paid tiers except quota and simultaneous relay session count.

Behavior after quota is exhausted:

- Do not disable terminal, file, API, or events.
- Throttle relay sessions to a low but terminal-usable speed.
- A starting throttle target can be `128 KB/s`; evaluate `64 KB/s` to `256 KB/s` after real testing.
- The product message should say this mode is suitable for emergency terminal access, not high-volume file transfer.

This also reduces the value of bypassing file download limits through terminal output. It does not make data exfiltration impossible, but it makes large data movement slow when over quota.

## Session-Level Limits

Real-time throttling should be enforced at the relay session / TURN allocation / managed WebRTC connection level.

User-level monthly quota remains the billing source of truth, but per-byte real-time throttling should not require every relay server worldwide to synchronously coordinate every packet.

Use two levels:

- User/month level: plan, monthly quota, cumulative relay usage, quota state.
- Relay session level: rate limit, current session usage, heartbeat, lifecycle, close/expire state.

Because session-level throttling can be bypassed by opening many parallel sessions, paid plans must also limit simultaneous relay sessions.

## Relay Session Lease Model

When a managed relay session is created, Control Plane or Hub should issue a short-lived relay session lease.

Suggested fields:

- `session_id`
- `user_id`
- `machine_id`
- `relay_id`
- `region`
- `plan`
- `quota_state`
- `rate_limit_bps`
- `concurrent_session_limit`
- `started_at`
- `expires_at`
- `last_heartbeat_at`

Suggested lifecycle:

1. User asks to establish managed connection.
2. Hub decides whether relay may be used.
3. Control Plane checks plan and active relay sessions.
4. If active relay sessions are at the limit, reject or ask user to close another relay session.
5. Insert an active relay session lease.
6. Hub/Relay receives session policy and optional TURN credentials.
7. Relay enforces session policy locally.
8. Relay sends heartbeat and byte deltas periodically.
9. On normal close, relay reports final usage and releases the session.
10. On crash/network loss, TTL expiry releases the session.

## Active Session Cleanup

Do not rely only on explicit close events. Use heartbeat plus TTL.

Suggested rules:

- Relay reports heartbeat every 10 seconds.
- Control Plane marks a session `expired` if no heartbeat arrives for 30 to 60 seconds.
- Normal close marks session `closed` immediately and settles final byte deltas.
- Background cleanup scans expired sessions periodically.

If a user exceeds concurrent session limit due to race conditions:

- Periodic reconciliation groups active sessions by user.
- Keep the earliest allowed sessions.
- Mark extra sessions `over_limit`.
- Relay can throttle or close `over_limit` sessions.

First implementation can be simpler:

- Transactional active-session count at connect time.
- Heartbeat every 10 seconds.
- Expire after 60 seconds without heartbeat.
- Close releases immediately.
- One background cleanup job.

## Multi-Region Relay

Do not make China, overseas, and other regional relays synchronously coordinate per-packet usage.

Recommended model:

- User connects to the nearest/selected Hub.
- Hub assigns a regional Relay/TURN server.
- Relay enforces its own session policy locally.
- Relay reports usage asynchronously to global Control Plane.
- Global Control Plane stores plan and monthly quota.

This tolerates cross-region latency and avoids putting global synchronization in the relay hot path.

Some quota overshoot is acceptable. A user may consume slightly more traffic near quota boundaries before the next heartbeat/report updates global state. This is a business tradeoff worth making to keep relay reliable.

## File Features And Bypass Reality

TermX should still build rich file manager features:

- Upload.
- Download.
- Background transfer progress.
- Copy file path.
- Preview small files.
- Directory browsing.
- File metadata.

But file capability should be policy-driven:

- `fileTransferAllowed`
- `filePreviewAllowed`
- `filePreviewMaxBytes`
- `backgroundTransferAllowed`
- `relayBytesRemaining`
- `relayThrottled`

If terminal access is allowed, it is impossible to fully prevent a user from outputting file contents through terminal commands. Product language should avoid claiming absolute download prevention.

Practical policy:

- In-quota paid relay can allow file transfer.
- Over-quota relay remains functional but slow.
- Small file preview can be a convenience feature, not the primary anti-download control.
- If preview is restricted by size, implement it as `preview(path)` with a server-side `stat` before read. Do not expose arbitrary range reads for preview-only plans.

## Capability Surface

The client should receive policy through `ConnectionCapabilities` or a related capability object. It should not infer policy from path names or plan names.

Suggested future fields:

```ts
interface ConnectionCapabilities {
  terminalAllowed: boolean
  apiAllowed: boolean
  eventsAllowed: boolean
  fileTransferAllowed: boolean
  terminalManagementAllowed: boolean
  relayInUse: boolean
  relayBytesIncluded?: number
  relayBytesUsed?: number
  relayBytesRemaining?: number
  relayThrottled?: boolean
  relayThrottleBps?: number
  relayConcurrentSessionLimit?: number
  relayActiveSessionCount?: number
  denialReason?: string
}
```

The exact TypeScript shape can evolve, but the boundary rule should not change: plan, quota, throttle, and relay state are capabilities/policy, not transport taxonomy.

## Implementation Notes For Future Work

- Add Control Plane model for plans and monthly relay quota.
- Add relay session lease table and heartbeat ingestion.
- Add Hub policy decision for `managed` connection setup.
- Add TURN credential issuance bound to user/session/relay policy.
- Add relay-side session accounting and rate limiting.
- Add quota reconciliation and expired-session cleanup.
- Extend frontend capabilities UI to explain relay usage, quota remaining, and over-quota throttling.
- Keep `local`, `public_p2p`, and `managed` as the only client-visible paths.
