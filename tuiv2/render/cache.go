package render

import (
	"sync"

	"github.com/lozzow/termx/tuiv2/runtime"
)

type runtimeLookup struct {
	terminals map[string]*runtime.VisibleTerminal
	paneRoles map[string]string
}

func newRuntimeLookup(runtimeState *VisibleRuntimeStateProxy) runtimeLookup {
	lookup := runtimeLookup{}
	if runtimeState == nil {
		return lookup
	}
	if len(runtimeState.Terminals) > 0 {
		lookup.terminals = make(map[string]*runtime.VisibleTerminal, len(runtimeState.Terminals))
		for i := range runtimeState.Terminals {
			terminal := &runtimeState.Terminals[i]
			lookup.terminals[terminal.TerminalID] = terminal
		}
	}
	if len(runtimeState.Bindings) > 0 {
		lookup.paneRoles = make(map[string]string, len(runtimeState.Bindings))
		for i := range runtimeState.Bindings {
			binding := runtimeState.Bindings[i]
			if binding.PaneID == "" || binding.Role == "" {
				continue
			}
			lookup.paneRoles[binding.PaneID] = binding.Role
		}
	}
	return lookup
}

func findVisibleTerminalWithLookup(lookup runtimeLookup, terminalID string) *runtime.VisibleTerminal {
	return lookup.terminal(terminalID)
}

func (l runtimeLookup) terminal(terminalID string) *runtime.VisibleTerminal {
	if terminalID == "" || len(l.terminals) == 0 {
		return nil
	}
	return l.terminals[terminalID]
}

func (l runtimeLookup) paneRole(paneID string) string {
	if paneID == "" || len(l.paneRoles) == 0 {
		return ""
	}
	return l.paneRoles[paneID]
}

var blankFillRowCache = struct {
	mu   sync.RWMutex
	rows map[int][]drawCell
}{
	rows: make(map[int][]drawCell),
}

var blankStringCache = struct {
	mu   sync.RWMutex
	rows map[int]string
}{
	rows: make(map[int]string),
}

func cachedBlankFillRow(width int) []drawCell {
	if width <= 0 {
		return nil
	}
	blankFillRowCache.mu.RLock()
	row := blankFillRowCache.rows[width]
	blankFillRowCache.mu.RUnlock()
	if row != nil {
		return row
	}
	row = make([]drawCell, width)
	row[0] = blankDrawCell()
	for filled := 1; filled < width; filled *= 2 {
		copy(row[filled:], row[:minInt(filled, width-filled)])
	}
	blankFillRowCache.mu.Lock()
	if cached := blankFillRowCache.rows[width]; cached != nil {
		row = cached
	} else {
		blankFillRowCache.rows[width] = row
	}
	blankFillRowCache.mu.Unlock()
	return row
}

func cachedBlankString(width int) string {
	if width <= 0 {
		return ""
	}
	blankStringCache.mu.RLock()
	row := blankStringCache.rows[width]
	blankStringCache.mu.RUnlock()
	if row != "" {
		return row
	}
	bytes := make([]byte, width)
	for i := range bytes {
		bytes[i] = ' '
	}
	row = string(bytes)
	blankStringCache.mu.Lock()
	if cached := blankStringCache.rows[width]; cached != "" {
		row = cached
	} else {
		blankStringCache.rows[width] = row
	}
	blankStringCache.mu.Unlock()
	return row
}
