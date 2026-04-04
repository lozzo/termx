# tuiv2 Mouse Support Matrix

Status legend:
- `implemented`: available and routed through render hit-regions + semantic actions.
- `partial`: works in some modes/layouts, or has notable UX gaps.
- `not yet implemented`: no current mouse path.

## Top Chrome

| Capability | Status | Interaction details | Likely next gap |
|---|---|---|---|
| Workspace label click -> open workspace picker | implemented | Click workspace label in row 0 opens `ModeWorkspacePicker`. | None critical. |
| Tab switch | implemented | Click tab token switches by index. | No tab-drag reorder. |
| Tab close | implemented | Click tab close token closes target tab. | No close confirmation for destructive path. |
| Tab create | implemented | Click `+` creates tab and opens picker for initial pane attach. | No alternate create modes from mouse (template/new terminal directly). |
| Tab rename / tab kill actions (`[tr]`/`[tx]`) | implemented | Routed via top-chrome action hit-regions to prompt/kill flow. | On narrow widths actions clip out; discoverability relies on available width. |
| Workspace prev/next/create/rename/delete (`[w<] [w>] [w+] [wr] [wx]`) | implemented | Routed via top-chrome action hit-regions and local/modal action handlers. | Same width clipping behavior; no overflow affordance. |

## Overlays / Modals

| Capability | Status | Interaction details | Likely next gap |
|---|---|---|---|
| Dismiss overlay by clicking outside card | implemented | Shared dismiss regions map to `ActionCancelMode`. | None critical. |
| Picker row click | implemented | Select row and submit attach/open create flow depending on row type. | No double-click shortcut variants. |
| Workspace picker row click | implemented | Select workspace row and submit switch/create flow. | None critical. |
| Picker/workspace/terminal-manager footer action buttons | implemented | Footer action rectangles come from shared layout; click dispatches semantic action directly. | Actions clip by width; no secondary access when hidden. |
| Prompt input click-to-place cursor | implemented | Prompt input hit-region maps x-position to cursor index. | No selection/drag highlight. |
| Prompt submit/cancel buttons | implemented | Separate hit-region kinds for submit/cancel, routed to modal action handler. | None critical. |
| Overlay wheel scrolling | partial | Picker + workspace picker support wheel navigation. Prompt/help/terminal-manager overlay wheel currently no-op. | Add wheel scroll for long help/detail content. |
| Query input click-to-focus/cursor for picker/workspace/terminal-manager | partial | Query row hit-regions exist, but clicks currently do not move query cursor or change focus semantics. | Add explicit query cursor model and click placement behavior. |

## Pane Chrome

| Capability | Status | Interaction details | Likely next gap |
|---|---|---|---|
| Tiled pane actions: close/zoom/split-v/split-h | implemented | Click top-border action slots; semantic actions dispatched centrally. | Width clipping can hide lower-priority controls. |
| Tiled attached-terminal actions: detach/reconnect/close+kill | implemented | Exposed when pane is terminal-attached context. | No explicit confirmation for close+kill. |
| Layout actions: balance/cycle | implemented | Exposed on layout-cluster pane chrome (cluster-limited). | Discoverability depends on which pane is cluster context. |
| Floating pane actions: close/zoom/open-picker/center/toggle-visibility | implemented | Exposed on floating pane top border; clicks focus/reorder before action dispatch. | No mouse toggle for pin/z-order strategy beyond focus reorder. |
| Owner action | implemented | Two-step owner confirm behavior via owner hit-region and confirm state. | Could add explicit visual timeout/progress indicator. |

## Pane Content

| Capability | Status | Interaction details | Likely next gap |
|---|---|---|---|
| Click to focus active pane | implemented | Clicking pane interior focuses pane (and floating pane reorders to top). | No marquee/selection semantics at app layer. |
| Terminal mouse forwarding (SGR) | implemented | Press/motion/release/wheel forwarded only for active pane content rect, only when terminal mouse tracking is enabled. UI chrome has priority over forwarding. | No forwarding for inactive pane content by design (could be made configurable). |
| Wheel scroll fallback (non-forwarded path) | implemented | When not forwarded, wheel on pane area updates tab scroll offset (with focus adjustments for floating target). | No per-pane independent scroll history controls via mouse gestures. |

## Terminal Pool Surface

| Capability | Status | Interaction details | Likely next gap |
|---|---|---|---|
| Row click select | implemented | Click list row sets selected item index. | No multi-select/batch actions. |
| Footer actions: attach-here/tab/floating/edit/kill | implemented | Footer action hit-regions dispatch modal actions directly. | Width clipping may hide low-priority actions. |
| Wheel scroll | implemented | Wheel moves selection within terminal pool list. | No page-jump gestures. |

## Dragging / Resizing

| Capability | Status | Interaction details | Likely next gap |
|---|---|---|---|
| Floating move drag | implemented | Left-press on floating title row starts move drag; motion updates rect with clamp to bounds. | No snap/grid/edge docking behavior. |
| Floating resize drag | implemented | Left-press near floating bottom-right corner starts resize drag; live resize + clamp. | No min/max handles beyond existing bounds logic. |
| Split divider drag resize | implemented | Left-press on split divider starts resize-split drag; motion updates split ratio. | No visual ghost/preview while dragging. |
| Tiled pane drag-to-move/reparent | not yet implemented | No drag semantics for tiled pane relocation. | If needed, add dedicated drag handles + drop targets. |
| Top-chrome drag (tab reorder/workspace reorder) | not yet implemented | No drag handlers for tab/workspace strip. | Add drag state model and render drop indicators. |

## Notes for Product Review

- The architecture is now consistently hit-region driven: render geometry defines clickable regions, app consumes region semantics.
- Most remaining gaps are UX/discoverability gaps under tight widths, plus missing drag-reorder and query-cursor mouse editing outside prompt mode.
