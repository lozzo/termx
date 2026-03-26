package pool

import (
	"sort"
	"strings"

	"github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

type Groups struct {
	Visible []terminal.Metadata
	Parked  []terminal.Metadata
	Exited  []terminal.Metadata
}

func BuildGroups(terminals map[types.TerminalID]terminal.Metadata, visibleTerminalIDs map[types.TerminalID]bool, query string) Groups {
	query = strings.TrimSpace(strings.ToLower(query))
	groups := Groups{}
	for terminalID, meta := range terminals {
		if !matchesQuery(meta, terminalID, query) {
			continue
		}
		switch {
		case meta.State == terminal.StateExited:
			groups.Exited = append(groups.Exited, meta)
		case visibleTerminalIDs != nil && visibleTerminalIDs[terminalID]:
			groups.Visible = append(groups.Visible, meta)
		default:
			groups.Parked = append(groups.Parked, meta)
		}
	}
	sortMetadata(groups.Visible)
	sortMetadata(groups.Parked)
	sortMetadata(groups.Exited)
	return groups
}

func matchesQuery(meta terminal.Metadata, terminalID types.TerminalID, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(string(terminalID)), query) {
		return true
	}
	if strings.Contains(strings.ToLower(meta.Name), query) {
		return true
	}
	for key, value := range meta.Tags {
		if strings.Contains(strings.ToLower(key), query) || strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func sortMetadata(items []terminal.Metadata) {
	sort.Slice(items, func(i, j int) bool {
		leftName := strings.TrimSpace(items[i].Name)
		rightName := strings.TrimSpace(items[j].Name)
		if leftName == rightName {
			return items[i].ID < items[j].ID
		}
		if leftName == "" {
			return false
		}
		if rightName == "" {
			return true
		}
		return leftName < rightName
	})
}
