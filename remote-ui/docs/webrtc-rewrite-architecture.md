# Archived: remote-ui WebRTC Rewrite Architecture

This document is retained only as historical context for the browser-side transport rewrite.

It is no longer the active planning document for current work.

Current work should follow:

- repository-root `AGENTS.md`
- repository-root `workflow.md`
- `remote-ui/AGENTS.md`
- `termx-remote/AGENTS.md`

Historical scope of this document:
- it described the interface-first browser transport rewrite
- it explained why runtime transport remains WebRTC DataChannel
- it recorded why relay is not a fourth client path

Current scope has moved forward:
- unified Hub / Agent / Web Controller product architecture
- stateless multi-region Hub design
- browser-side integration against the unified remote product flow
- future-native boundary retention without implementing native yet
