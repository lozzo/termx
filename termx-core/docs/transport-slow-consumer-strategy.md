# Slow-Consumer Screen Update Strategy

## Goal

When a subscriber is behind, prioritize delivering the newest usable terminal state instead of preserving every intermediate screen frame. The exception is normal-screen scrollback: it must remain semantically correct even when intermediate frames are collapsed.

## Where Backlog Is Handled

- `terminal.go`
  - Live screen updates now choose between delta encoding and full-replace encoding based on change density, scroll behavior, and encoded byte size.
- `termx.go` / attachment stream pump
  - Each attached subscriber now has an explicit outbound queue.
  - The queue tracks the latest screen state for that subscriber and collapses a trailing run of queued screen updates when the subscriber is slow.
- `fanout`
  - still records upstream subscriber backlog depth with `fanout.subscriber.backlog.frames`
  - the attachment pump is intended to keep this backlog shallow by draining the fanout stream promptly
- `transport`
  - Wire bytes are counted with `transport.bytes_over_wire` and `transport.stream.bytes_over_wire`.

## Collapse Policy

### Alternate Screen

- Collapse aggressively.
- Thresholds:
  - queued frames >= `2`
  - or queued bytes >= `16 KiB`
- Allowed strategy:
  - replace a trailing run of queued screen updates with one merged latest-state update
  - use lightweight full-replace because alt-screen has no scrollback contract to preserve

### Normal Screen

- Collapse conservatively.
- Thresholds:
  - queued frames >= `4`
  - or queued bytes >= `64 KiB`
- Allowed strategy:
  - replace a trailing run of queued screen updates with one merged latest-state update
  - the merged update must use `ResetScrollback + full scrollback replay`
- Reason:
  - current client/runtime full-replace path does not implicitly retain old scrollback, so normal-screen collapse must replay the authoritative scrollback state explicitly

## Encoding Strategy

For each live update, the server now compares:

- delta update
  - span diff for sparse edits
  - scroll/copy opcodes when large moves are detected
- full replace
  - favored when density is high or the encoded full replace is cheaper

Rules:

- sparse change: prefer delta
- large-range scroll: prefer delta with scroll opcode when it stays close to or below full-replace cost
- dense change:
  - alt screen: bias toward full replace sooner
  - normal screen: only use full replace when the payload is still favorable after accounting for scrollback correctness

## When Intermediate Frames May Be Dropped

- Allowed:
  - alternate-screen redraw churn where only the newest screen matters
  - normal-screen backlog collapse when the replacement frame replays the exact latest scrollback plus latest visible screen
  - repeated queued screen updates in the same trailing screen-update suffix

- Not allowed:
  - dropping normal-screen history without replaying the missing scrollback rows
  - reordering resize/bootstrap/closed frames around queued screen updates
  - collapsing across non-screen barriers that change ordering semantics

## Metrics

- `transport.bytes_over_wire`
  - all server-to-client bytes
- `transport.stream.bytes_over_wire`
  - attached stream bytes only
- `fanout.subscriber.backlog.frames`
  - sampled upstream fanout buffer depth
- `transport.stream.backlog.coalesced_frames`
  - how many queued screen frames were removed
- `transport.stream.backlog.saved_bytes`
  - estimated queued bytes saved by collapse
- `transport.stream.backlog.collapse.alternate`
  - alternate-screen collapse events
- `transport.stream.backlog.collapse.normal`
  - normal-screen collapse events
- `terminal.screen_update.encode_mode.delta`
  - bytes sent with delta encoding
- `terminal.screen_update.encode_mode.full_replace`
  - bytes sent with full-replace encoding

## Slow-Link Comparison

Measured from `go test -run 'TestTransportSlowConsumer(AltScreenCoalescesToLatestState|NormalScreenPreservesScrollback)$' -v .`

| Scenario | Fast Link | Slow Link | Effect |
| --- | ---: | ---: | --- |
| alt-screen churn | `41770 B`, `10` screen updates | `5402 B`, `2` screen updates | newest frame wins, intermediate redraws collapsed |
| normal-screen scroll | `2510 B`, `15` screen updates | `1122 B`, `2` screen updates | scrollback preserved while intermediate frames collapsed |

## Operational Summary

- Slow subscribers no longer force the server to ship every intermediate fullscreen redraw.
- Alternate-screen sessions can skip to the latest visual state aggressively.
- Normal-screen sessions can still skip intermediate frames, but only by replaying the authoritative latest scrollback and screen state together.
