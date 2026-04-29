# Screen Update Opcode Prototype

## Goal

Prototype a damage/opcode-based screen update payload for incremental terminal updates, instead of depending only on "previous frame vs next frame row diff".

This prototype keeps the existing `TypeScreenUpdate` stream frame and adds a new binary payload version, `TSU4`, while preserving decode fallback for `TSU2` and `TSU3`.

## x/vt input source

Upstream `github.com/charmbracelet/x/vt v0.0.0-20260316093931-f2fb44ab3145` defines damage types, but it does not expose a consumable damage stream/callback from the emulator.

`termx` now carries a local fork under:

- `third_party/github.com/charmbracelet/x/vt`

That local fork adds:

- `Emulator.WriteWithDamage`
- `SafeEmulator.WriteWithDamage`
- structured screen damages recorded at the `Screen` layer:
  - `SpanDamage`
  - `ClearDamage`
  - `ScrollDamage`
  - existing movement damage types

`vterm` now prefers this direct damage stream when available, and falls back to:

- `Touched()` rows from `x/vt`
- cached screen rows / row fingerprints already maintained in `vterm`
- the old planner that upgrades row-aligned moves into opcodes

## TSU4 payload draft

`TSU4` keeps the same high-level header as `TSU3`:

- `flags`
- `size`
- optional `screen_scroll`
- optional `title`
- `modes`
- `cursor`
- style table
- scrollback trim / append

The change is the delta body:

- `TSU3`: changed spans
- `TSU4`: opcode sequence

### Opcodes

- `WriteSpan`
  - `row`, `col`, `cells`, `timestamp`, `row_kind`
- `ScrollRect`
  - `rect`, `dx`, `dy`
- `CopyRect`
  - `src_rect`, `dst_x`, `dst_y`
- `ClearRect`
  - `rect`, `timestamp`, `row_kind`
- `ClearToEOL`
  - `row`, `col`, `timestamp`, `row_kind`
- `Cursor`
  - full cursor state
- `Modes`
  - full terminal mode mask
- `Resize`
  - `cols`, `rows`
- `Title`
  - title string

## Encode/decode/apply prototype

### Server side

- `vterm.WriteWithDamage` still starts from `Touched()` + cached screen state.
- On the local `x/vt` fork path, `vterm` now first asks the emulator for direct damages with `WriteWithDamage`.
- The planner now emits:
  - full-screen `ScrollRect` for classic scroll shifts
  - row-aligned `ScrollRect` / `CopyRect` candidates for large vertical moves
  - grouped `ClearRect` for repeated clear bands
  - residual `WriteSpan` / `ClearToEOL`
- `terminal.screenUpdatePayloadFromDamageLocked` converts those ops to protocol `ScreenOp`s
- `protocol.EncodeScreenUpdatePayload` now picks the smaller safe encoding between:
  - `TSU4` when opcode form wins
  - `TSU3` when row/span form is already better or equal

### Client/runtime side

- `protocol.DecodeScreenUpdatePayload` now accepts:
  - `TSU2`
  - `TSU3`
  - `TSU4`
- `tuiv2/runtime/applyScreenUpdateSnapshot` applies `TSU4` ops directly for snapshot maintenance.
- `vterm.ApplyScreenUpdate` applies `TSU4` ops directly to the local emulator-backed screen state instead of forcing everything back through row spans.

## Why this fits scroll/copy better

Row/span diff is good for sparse writes. It is not a natural representation for:

- scroll regions
- full-screen scroll
- large block copies
- wide clear bands

For those workloads, opcode form has two structural advantages:

- Wire size scales with the move description, not with every destination row.
- Apply can preserve movement semantics directly, especially for partial scroll regions and block copies.

This is most visible for:

- `vim`-style scroll regions
- window/pane content moves
- repeated multi-row clears

It is less compelling for:

- sparse single-point updates
- already-optimized full-screen scroll paths where old `ScreenScroll` had a dedicated fast path

## Benchmarks

Command:

```bash
go test ./tuiv2/runtime -run '^$' -bench BenchmarkScreenUpdateOpcodeScenarios -benchmem -count=1
go test ./tuiv2/runtime -run TestScreenUpdateOpcodeScenarioWireSizes -v
```

Environment:

- `darwin/arm64`
- `Apple M2`

### Decode + snapshot apply

| Scenario | Legacy | Opcode |
| --- | ---: | ---: |
| `less_scroll` | `1620 ns/op`, `4976 B/op`, `15 allocs/op` | `1684 ns/op`, `4976 B/op`, `15 allocs/op` |
| `vim_scroll_region` | `231523 ns/op`, `1049299 B/op`, `150 allocs/op` | `35398 ns/op`, `203352 B/op`, `41 allocs/op` |
| `top_scroll` | `9614 ns/op`, `42800 B/op`, `28 allocs/op` | `9654 ns/op`, `42800 B/op`, `28 allocs/op` |
| `block_move` | `113205 ns/op`, `501537 B/op`, `79 allocs/op` | `14692 ns/op`, `86648 B/op`, `25 allocs/op` |
| `sparse_point` | `4431 ns/op`, `19072 B/op`, `14 allocs/op` | `4442 ns/op`, `19072 B/op`, `14 allocs/op` |

### Wire size

| Scenario | Legacy bytes | Opcode bytes |
| --- | ---: | ---: |
| `less_scroll` | `71` | `71` |
| `vim_scroll_region` | `10379` | `544` |
| `top_scroll` | `373` | `373` |
| `block_move` | `4956` | `51` |
| `sparse_point` | `43` | `43` |

## Reading the results

The prototype now has a non-regression property for the simple cases:

- full-screen scroll now falls back to `TSU3` when opcode form is not better
- sparse single-point updates also fall back to `TSU3`

And it still proves the main point for partial-scroll and block-move workloads:

- `vim_scroll_region`: much smaller wire payload, much lower decode/apply cost
- `block_move`: dramatically smaller wire payload and much lower decode/apply cost

So the present state is:

- wire semantics for scroll/copy/clear are explicit
- partial scroll/copy workloads benefit immediately
- simple full-screen and sparse cases no longer regress because the encoder picks `TSU3` when that is the better representation

## Files touched by the prototype

- `protocol/messages.go`
- `terminal.go`
- `vterm/vterm.go`
- `tuiv2/runtime/screen_update_contract.go`
- `tuiv2/runtime/snapshot.go`
- tests and benchmarks under `protocol/`, `vterm/`, and `tuiv2/runtime/`
