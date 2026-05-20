package runtime

import "strings"

const terminalPaneSurfacePrefix = "tui:pane:"

func TerminalPaneSurfaceID(paneID string) string {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return ""
	}
	return terminalPaneSurfacePrefix + paneID
}

func paneIDFromTerminalSurfaceID(surfaceID string) string {
	return strings.TrimPrefix(strings.TrimSpace(surfaceID), terminalPaneSurfacePrefix)
}
