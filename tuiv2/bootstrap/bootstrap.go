// Package bootstrap implements the minimal startup sequence for the TUI v2.
// It is responsible for two operations:
//
//   - Startup: initialise a fresh workbench when there is no prior state.
//   - Restore: deserialise persisted V2 state into a workbench.
//
// The package intentionally has no compile-time dependency on the old tui/
// package and must be able to operate independently of it.
package bootstrap

import (
	"errors"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// Config carries optional startup preferences. It is intentionally sparse;
// additional fields can be added without breaking callers.
type Config struct {
	// DefaultWorkspaceName overrides the name used for the first workspace
	// created by Startup. When empty, "main" is used.
	DefaultWorkspaceName string

	// DefaultTabName overrides the name of the first tab. When empty, "1" is
	// used.
	DefaultTabName string
}

// StartupResult is returned by Startup and describes what the caller should
// do next.
type StartupResult struct {
	// ShouldOpenPicker is true when Startup has created a blank workspace and
	// the caller should present the workspace/terminal picker to the user.
	ShouldOpenPicker bool

	// PanesToReattach lists panes that had a persisted TerminalID and should
	// be automatically re-attached on startup.  Empty on a fresh Startup.
	PanesToReattach []PaneReattachHint
}

// PaneReattachHint identifies a pane that should be re-attached to a terminal.
type PaneReattachHint struct {
	TabID      string
	PaneID     string
	TerminalID string
}

// Startup initialises wb with a single default workspace and tab, ready for
// the user to start working. It always sets ShouldOpenPicker=true because
// there are no live terminals yet.
//
// rt is accepted for future use (e.g. creating an initial terminal during
// startup) and may be nil.
func Startup(cfg Config, wb *workbench.Workbench, _ *runtime.Runtime) (StartupResult, error) {
	wsName := cfg.DefaultWorkspaceName
	if wsName == "" {
		wsName = "main"
	}
	tabName := cfg.DefaultTabName
	if tabName == "" {
		tabName = "1"
	}

	paneID := "1"
	shared.ObservePaneID(paneID)
	tabID := "1"
	shared.ObserveTabID(tabID)
	tab := &workbench.TabState{
		ID:           tabID,
		Name:         tabName,
		Panes:        map[string]*workbench.PaneState{paneID: {ID: paneID}},
		Root:         workbench.NewLeaf(paneID),
		ActivePaneID: paneID,
	}

	ws := &workbench.WorkspaceState{
		Name:      wsName,
		Tabs:      []*workbench.TabState{tab},
		ActiveTab: 0,
	}

	wb.AddWorkspace(wsName, ws)

	return StartupResult{ShouldOpenPicker: true}, nil
}

func RestoreOrStartup(data []byte, cfg Config, wb *workbench.Workbench, rt *runtime.Runtime) (StartupResult, error) {
	if len(data) == 0 {
		return Startup(cfg, wb, rt)
	}
	if err := Restore(data, wb, rt); err != nil {
		if errors.Is(err, ErrEmptyData) {
			return Startup(cfg, wb, rt)
		}
		return StartupResult{}, err
	}
	if wb == nil || wb.CurrentWorkspace() == nil {
		return Startup(cfg, wb, rt)
	}
	return StartupResult{PanesToReattach: collectReattachHints(wb)}, nil
}

// collectReattachHints walks all workspaces and returns a hint for every pane
// that has a non-empty TerminalID (i.e. was bound to a terminal at save time).
func collectReattachHints(wb *workbench.Workbench) []PaneReattachHint {
	if wb == nil {
		return nil
	}
	original := ""
	if current := wb.CurrentWorkspace(); current != nil {
		original = current.Name
	}
	defer func() {
		if original != "" {
			wb.SwitchWorkspace(original)
		}
	}()

	var hints []PaneReattachHint
	for _, wsName := range wb.ListWorkspaces() {
		if !wb.SwitchWorkspace(wsName) {
			continue
		}
		ws := wb.CurrentWorkspace()
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			for _, pane := range tab.Panes {
				if pane != nil && pane.TerminalID != "" {
					hints = append(hints, PaneReattachHint{
						TabID:      tab.ID,
						PaneID:     pane.ID,
						TerminalID: pane.TerminalID,
					})
				}
			}
		}
	}
	return hints
}
