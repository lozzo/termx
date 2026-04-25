# Resize Preview Reflow Worklog

## Requirement Summary

Implement pane resize preview reflow for `termx` `tuiv2`.

When a pane is resized, the client layout can change before the real PTY/application receives resize/SIGWINCH and emits a redraw. During that gap, `termx` should render a local temporary preview derived from a captured preview source rather than exposing a lossy live vterm intermediate state.

Expected behavior:

- Non-alt-screen terminal pages reflow ordinary terminal text to the current pane width during preview.
- Alt-screen/fullscreen pages do not text-reflow; they preserve 2D grid semantics with crop on shrink and restore on expand.
- Continuous shrink/expand operations regenerate preview from the original preview source captured when entering preview, not from previous provisional previews.
- Real PTY/application output exits preview and shows the real terminal state.
- Resize echo, viewport-only, cursor-only, metadata-only, or layout-only updates must not exit preview.
- Render / Visible / projection paths must stay pure read-only.
- Screen update / snapshot / bootstrap transport must remain binary encoded.

## Current Branch

`feature/tuiv2-resize-preview-reflow`

## TODO

- [x] 1. Create branch and worklog
- [ ] 2. Inspect existing resize pipeline
- [ ] 3. Study tmux / terminal resize behavior
- [ ] 4. Reproduce current missing capability in tmux
- [ ] 5. Design preview source and lifecycle
- [ ] 6. Implement non-alt-screen reflow preview
- [ ] 7. Implement alt-screen crop/restore preview
- [ ] 8. Implement preview exit on real output
- [ ] 9. Add runtime tests
- [ ] 10. Add render tests
- [ ] 11. Validate with tmux capture
- [ ] 12. Run final tests/build
- [ ] 13. Write final summary

## Phase Log

### 1. Create branch and worklog

Commands:

```sh
git status --short
git branch --show-current
git switch -c feature/tuiv2-resize-preview-reflow || git switch -c feature/tuiv2-resize-preview-reflow-$(date +%Y%m%d)
mkdir -p docs/agent-worklogs
cat > docs/agent-worklogs/resize-preview-reflow-2026-04-25.md
```

Results:

- Started from `master`.
- Created branch `feature/tuiv2-resize-preview-reflow`.
- Created this worklog at `docs/agent-worklogs/resize-preview-reflow-2026-04-25.md`.

Capture files:

- None yet.

Commit:

- Pending at time of writing this section.

## Investigation Notes

Pending.

## Tmux Reproduction Notes

Pending.

## Design Notes

Pending.

## Implementation Notes

Pending.

## Test and Validation Notes

Pending.

## Known Issues

- None yet.

## Next Step Suggestions

- Inspect existing resize/runtime/render/vterm code paths.
- Study tmux source and observed behavior for resize reflow versus alt-screen crop.
- Build a local `./termx` binary before tmux reproduction if needed.

## Resume From Here

Current status:

- Branch: `feature/tuiv2-resize-preview-reflow`
- Last completed TODO: `1. Create branch and worklog`
- Last commit: pending initial worklog commit
- Next step: inspect existing resize pipeline in `tuiv2/runtime`, `tuiv2/app`, `tuiv2/render`, `vterm`, and tmux source references.

Important artifacts:

- `docs/agent-worklogs/resize-preview-reflow-2026-04-25.md`
