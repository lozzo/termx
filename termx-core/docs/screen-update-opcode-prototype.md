# Screen Update Opcodes

TermX now uses one screen update wire format: `TSU6`.

`TSU6` carries:

- header flags, size, optional `screen_scroll`, optional title
- terminal modes and cursor
- style table
- optional full-replace screen rows and metadata
- `ScreenOp` delta sequence
- scrollback trim / append rows

Old row/span payload models (`TSU2`, `TSU3`, `TSU4`, `TSU5`, `changed_rows`, `changed_spans`) have been removed. New code should emit either a full replace or `ScreenOp` deltas.

## Opcodes

- `WriteSpan`: row, col, cells, timestamp, row kind, wrapped metadata
- `ScrollRect`: rect, dx, dy
- `CopyRect`: source rect, destination x/y
- `ClearRect`: rect, timestamp, row kind, wrapped metadata
- `ClearToEOL`: row, col, timestamp, row kind, wrapped metadata
- `Cursor`: full cursor state
- `Modes`: full terminal mode mask
- `Resize`: cols, rows
- `Title`: title string

## Runtime Model

- PTY bytes are written into the server authoritative `VTerm`.
- `VTerm.WriteWithDamage` consumes direct damages from the local `x/vt` fork and converts them to `ScreenOp`s.
- Unsupported or geometry-changing damage falls back to a full replace.
- Attach streams collapse queued screen state so slow consumers receive the newest recoverable screen state instead of every intermediate frame.
- Clients apply `ScreenOp`s to their local snapshot/VTerm and render from that state.

This matches the tmux-style direction: keep an authoritative server-side screen, send structural screen changes, and avoid appending raw PTY output line by line through every client.
