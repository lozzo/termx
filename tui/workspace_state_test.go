package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
)

func TestParseWorkspaceStateJSONAllowsTrailingGarbageAfterFirstObject(t *testing.T) {
	state, err := parseWorkspaceStateJSON([]byte(`{
  "version": 1,
  "workspace": {
    "name": "main",
    "active_tab": 0,
    "tabs": []
  }
}
noise`))
	if err != nil {
		t.Fatalf("expected trailing garbage to be ignored, got %v", err)
	}
	if state.Workspace.Name != "main" {
		t.Fatalf("expected workspace main, got %q", state.Workspace.Name)
	}
}

func TestLoadWorkspaceStateCmdRestoresRunningAndMissingPanes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, "workspace-state.json")
	if err := os.WriteFile(statePath, []byte(`{
  "version": 1,
  "workspace": {
    "name": "main",
    "active_tab": 0,
    "tabs": [
      {
        "name": "coding",
        "active_pane_id": "pane-2",
        "floating_visible": true,
        "root": {
          "direction": "vertical",
          "ratio": 0.5,
          "first": {"pane_id": "pane-1"},
          "second": {"pane_id": "pane-2"}
        },
        "panes": [
          {
            "id": "pane-1",
            "title": "editor",
            "terminal_id": "term-001",
            "command": ["vim", "."],
            "tags": {"role": "editor"},
            "terminal_state": "running",
            "mode": "fit"
          },
          {
            "id": "pane-2",
            "title": "logs",
            "terminal_id": "term-missing",
            "command": ["tail", "-f", "app.log"],
            "tags": {"role": "log"},
            "terminal_state": "running",
            "mode": "fixed",
            "offset": {"x": 4, "y": 2},
            "pin": true,
            "readonly": true
          }
        ],
        "floating": [
          {"pane_id": "pane-2", "rect": {"x": 8, "y": 3, "w": 30, "h": 10}, "z": 0}
        ]
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "term-001",
				Name:    "editor",
				Command: []string{"vim", "."},
				State:   "running",
				Tags:    map[string]string{"role": "editor"},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.loadWorkspaceStateCmd(statePath))
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.workspace.Name != "main" {
		t.Fatalf("expected workspace main, got %q", model.workspace.Name)
	}
	tab := model.currentTab()
	if tab == nil || tab.Name != "coding" {
		t.Fatalf("expected coding tab, got %#v", tab)
	}
	if tab.ActivePaneID != "pane-2" {
		t.Fatalf("expected active pane pane-2, got %q", tab.ActivePaneID)
	}
	if len(tab.Floating) != 1 || tab.Floating[0].PaneID != "pane-2" {
		t.Fatalf("expected floating pane pane-2, got %#v", tab.Floating)
	}
	running := tab.Panes["pane-1"]
	if running == nil || running.TerminalID != "term-001" {
		t.Fatalf("expected running pane to attach term-001, got %#v", running)
	}
	missing := tab.Panes["pane-2"]
	if missing == nil {
		t.Fatal("expected missing pane")
	}
	if missing.TerminalState != "exited" {
		t.Fatalf("expected missing pane to degrade to exited, got %q", missing.TerminalState)
	}
	if missing.Mode != ViewportModeFixed || !missing.Pin || !missing.Readonly || missing.Offset != (Point{X: 4, Y: 2}) {
		t.Fatalf("expected fixed pane flags to round-trip, got %#v", missing.Viewport)
	}
}

func TestInitRestoresWorkspaceStateBeforeAutoLayoutAndStartupPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, "workspace-state.json")
	if err := os.WriteFile(statePath, []byte(`{
  "version": 1,
  "workspace": {
    "name": "state-ws",
    "active_tab": 0,
    "tabs": [
      {
        "name": "state-tab",
        "active_pane_id": "pane-1",
        "floating_visible": true,
        "root": {"pane_id": "pane-1"},
        "panes": [
          {
            "id": "pane-1",
            "title": "state-pane",
            "terminal_id": "term-001",
            "command": ["vim", "."],
            "tags": {"role": "editor"},
            "terminal_state": "running",
            "mode": "fit"
          }
        ]
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	projectDir := t.TempDir()
	t.Chdir(projectDir)
	if err := os.MkdirAll(filepath.Join(projectDir, ".termx"), 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".termx", "layout.yaml"), []byte(`
name: auto
tabs:
  - name: auto-tab
    tiling:
      terminal:
        command: "echo auto"
`), 0o644); err != nil {
		t.Fatalf("write project layout: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-001", Name: "editor", Command: []string{"vim", "."}, State: "running", Tags: map[string]string{"role": "editor"}},
		},
	}
	model := NewModel(client, Config{
		DefaultShell:       "/bin/sh",
		StartupPicker:      true,
		WorkspaceStatePath: statePath,
		StartupAutoLayout:  true,
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.terminalPicker != nil {
		t.Fatal("expected workspace state to bypass startup picker")
	}
	if model.workspace.Name != "state-ws" {
		t.Fatalf("expected workspace state to win, got %q", model.workspace.Name)
	}
	if tab := model.currentTab(); tab == nil || tab.Name != "state-tab" {
		t.Fatalf("expected restored state tab, got %#v", tab)
	}
}

func TestInitWithEmptyWorkspaceStateFallsBackToStartupPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, "workspace-state.json")
	if err := os.WriteFile(statePath, []byte(`{
  "version": 1,
  "workspace": {
    "name": "empty-ws",
    "active_tab": 0,
    "tabs": [
      {
        "name": "empty-tab",
        "floating_visible": true,
        "panes": []
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	model := NewModel(&fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}, Config{
		DefaultShell:       "/bin/sh",
		StartupPicker:      true,
		WorkspaceStatePath: statePath,
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.workspace.Name != "empty-ws" {
		t.Fatalf("expected empty workspace state to be restored first, got %q", model.workspace.Name)
	}
	if model.terminalPicker == nil {
		t.Fatal("expected empty workspace state to fall back to startup picker")
	}
	if model.terminalPicker.Title != "Choose Terminal" {
		t.Fatalf("expected startup picker title, got %q", model.terminalPicker.Title)
	}
	if tab := model.currentTab(); tab == nil || len(tab.Panes) != 0 {
		t.Fatalf("expected empty workspace tab to remain empty before picker selection, got %#v", tab)
	}
}

func TestInitWithInvalidWorkspaceStateFallsBackToStartupPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, "workspace-state.json")
	if err := os.WriteFile(statePath, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	model := NewModel(&fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}, Config{
		DefaultShell:       "/bin/sh",
		StartupPicker:      true,
		WorkspaceStatePath: statePath,
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.terminalPicker == nil {
		t.Fatal("expected invalid workspace state to fall back to startup picker")
	}
	if model.err != nil {
		t.Fatalf("expected invalid workspace state to be downgraded, got %v", model.err)
	}
}

func TestInitWithEmptyWorkspaceStateAndNoTerminalsCreatesFirstPane(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, "workspace-state.json")
	if err := os.WriteFile(statePath, []byte(`{
  "version": 1,
  "workspace": {
    "name": "empty-ws",
    "active_tab": 0,
    "tabs": [
      {
        "name": "empty-tab",
        "floating_visible": false,
        "panes": []
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	model := NewModel(&fakeClient{}, Config{
		DefaultShell:       "/bin/sh",
		StartupPicker:      true,
		WorkspaceStatePath: statePath,
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, nextCmd := model.Update(next)
			if nextCmd != nil {
				if follow := mustRunCmd(t, nextCmd); follow != nil {
					_, _ = model.Update(follow)
				}
			}
		}
	}

	tab := model.currentTab()
	if tab == nil || len(tab.Panes) != 0 {
		t.Fatalf("expected empty workspace startup to wait for explicit terminal creation, got %#v", tab)
	}
	if model.terminalPicker == nil || len(model.terminalPicker.Filtered) != 1 || !model.terminalPicker.Filtered[0].CreateNew {
		t.Fatalf("expected empty workspace startup to open create-only terminal picker, got %#v", model.terminalPicker)
	}
}

func TestInitUsesProjectAutoLayoutWhenNoWorkspaceState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectDir := t.TempDir()
	t.Chdir(projectDir)
	if err := os.MkdirAll(filepath.Join(projectDir, ".termx"), 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".termx", "layout.yaml"), []byte(`
name: auto
tabs:
  - name: auto-tab
    tiling:
      terminal:
        tag: "role=editor"
        command: "vim ."
`), 0o644); err != nil {
		t.Fatalf("write project layout: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-001", Name: "editor", Command: []string{"vim", "."}, State: "running", Tags: map[string]string{"role": "editor"}},
		},
	}
	model := NewModel(client, Config{
		DefaultShell:       "/bin/sh",
		StartupPicker:      true,
		WorkspaceStatePath: filepath.Join(home, "missing-state.json"),
		StartupAutoLayout:  true,
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.terminalPicker != nil {
		t.Fatal("expected auto layout to bypass startup picker")
	}
	if model.workspace.Name != "auto" {
		t.Fatalf("expected project auto layout workspace, got %q", model.workspace.Name)
	}
	tab := model.currentTab()
	if tab == nil || tab.Name != "auto-tab" {
		t.Fatalf("expected auto-tab, got %#v", tab)
	}
	if pane := tab.Panes[tab.ActivePaneID]; pane == nil || pane.TerminalID != "term-001" {
		t.Fatalf("expected auto layout to attach matching terminal, got %#v", pane)
	}
}

func TestInitUsesUserDefaultAutoLayoutWhenProjectLayoutMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectDir := t.TempDir()
	t.Chdir(projectDir)

	userLayoutDir := filepath.Join(home, ".config", "termx")
	if err := os.MkdirAll(userLayoutDir, 0o755); err != nil {
		t.Fatalf("mkdir user layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userLayoutDir, "default-layout.yaml"), []byte(`
name: user-default
tabs:
  - name: user-tab
    tiling:
      terminal:
        tag: "role=ops"
        command: "htop"
`), 0o644); err != nil {
		t.Fatalf("write user default layout: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-ops", Name: "ops", Command: []string{"htop"}, State: "running", Tags: map[string]string{"role": "ops"}},
		},
	}
	model := NewModel(client, Config{
		DefaultShell:       "/bin/sh",
		StartupPicker:      true,
		WorkspaceStatePath: filepath.Join(home, "missing-state.json"),
		StartupAutoLayout:  true,
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.workspace.Name != "user-default" {
		t.Fatalf("expected user default layout workspace, got %q", model.workspace.Name)
	}
	tab := model.currentTab()
	if tab == nil || tab.Name != "user-tab" {
		t.Fatalf("expected user-tab, got %#v", tab)
	}
	if pane := tab.Panes[tab.ActivePaneID]; pane == nil || pane.TerminalID != "term-ops" {
		t.Fatalf("expected user default layout to attach matching terminal, got %#v", pane)
	}
}

func TestSaveWorkspaceStateCmdWritesJSONFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.workspace = Workspace{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*Tab{
			{
				Name:            "coding",
				ActivePaneID:    "pane-1",
				FloatingVisible: false,
				Root:            NewLeaf("pane-1"),
				Panes: map[string]*Pane{
					"pane-1": {
						ID:    "pane-1",
						Title: "editor",
						Viewport: &Viewport{
							TerminalID:    "term-001",
							Command:       []string{"vim", "."},
							Tags:          map[string]string{"role": "editor"},
							TerminalState: "running",
							Mode:          ViewportModeFit,
							renderDirty:   true,
						},
					},
				},
			},
		},
	}

	statePath := filepath.Join(home, "workspace-state.json")
	msg := mustRunCmd(t, model.saveWorkspaceStateCmd(statePath))
	if notice, ok := msg.(noticeMsg); !ok || notice.text == "" {
		t.Fatalf("expected save notice, got %#v", msg)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected workspace state JSON")
	}
}

func TestPersistWorkspaceStateWritesFinalModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.workspace = Workspace{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*Tab{
			{
				Name:            "coding",
				ActivePaneID:    "pane-1",
				FloatingVisible: true,
				Root:            NewLeaf("pane-1"),
				Panes: map[string]*Pane{
					"pane-1": {
						ID:    "pane-1",
						Title: "editor",
						Viewport: &Viewport{
							TerminalID:    "term-001",
							Command:       []string{"vim", "."},
							TerminalState: "running",
							Mode:          ViewportModeFit,
							renderDirty:   true,
						},
					},
				},
			},
		},
	}

	statePath := filepath.Join(home, "workspace-state.json")
	if err := persistWorkspaceState(model, statePath, nil); err != nil {
		t.Fatalf("persistWorkspaceState returned error: %v", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected persisted state file: %v", err)
	}
}

func TestSaveWorkspaceStateCmdPersistsMultipleWorkspaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh", Workspace: "main"})
	model.workspace = Workspace{
		Name:      "workspace-2",
		ActiveTab: 0,
		Tabs: []*Tab{
			{
				Name:            "secondary",
				ActivePaneID:    "pane-2",
				FloatingVisible: true,
				Root:            NewLeaf("pane-2"),
				Panes: map[string]*Pane{
					"pane-2": {
						ID:    "pane-2",
						Title: "secondary-pane",
						Viewport: &Viewport{
							TerminalID:    "term-001",
							Command:       []string{"tail", "-f", "worker.log"},
							TerminalState: "running",
							Mode:          ViewportModeFit,
							renderDirty:   true,
						},
					},
				},
			},
		},
	}
	model.workspaceStore = map[string]Workspace{
		"main": {
			Name:      "main",
			ActiveTab: 0,
			Tabs: []*Tab{
				{
					Name:            "primary",
					ActivePaneID:    "pane-1",
					FloatingVisible: true,
					Root:            NewLeaf("pane-1"),
					Panes: map[string]*Pane{
						"pane-1": {
							ID:    "pane-1",
							Title: "primary-pane",
							Viewport: &Viewport{
								TerminalID:    "term-101",
								Command:       []string{"vim", "."},
								TerminalState: "running",
								Mode:          ViewportModeFit,
								renderDirty:   true,
							},
						},
					},
				},
			},
		},
		"workspace-2": model.workspace,
	}
	model.workspaceOrder = []string{"main", "workspace-2"}
	model.activeWorkspace = 1

	statePath := filepath.Join(home, "workspace-state.json")
	msg := mustRunCmd(t, model.saveWorkspaceStateCmd(statePath))
	if _, ok := msg.(noticeMsg); !ok {
		t.Fatalf("expected notice message, got %#v", msg)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if !strings.Contains(string(data), `"workspaces"`) || !strings.Contains(string(data), `"active_workspace": 1`) {
		t.Fatalf("expected multi-workspace state, got:\n%s", string(data))
	}

	var state workspaceStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal workspace state: %v", err)
	}
	if len(state.Workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %#v", state.Workspaces)
	}
	if state.Workspaces[0].Name != "main" || len(state.Workspaces[0].Tabs) != 1 || state.Workspaces[0].Tabs[0].Name != "primary" {
		t.Fatalf("expected saved main workspace to keep original tab tree, got %#v", state.Workspaces[0])
	}
	if len(state.Workspaces[0].Tabs[0].Panes) != 1 || state.Workspaces[0].Tabs[0].Panes[0].Title != "primary-pane" || state.Workspaces[0].Tabs[0].Panes[0].TerminalID != "term-101" {
		t.Fatalf("expected saved main workspace pane metadata to remain distinct, got %#v", state.Workspaces[0].Tabs[0].Panes)
	}
	if state.Workspaces[1].Name != "workspace-2" || len(state.Workspaces[1].Tabs) != 1 || state.Workspaces[1].Tabs[0].Name != "secondary" {
		t.Fatalf("expected saved secondary workspace to remain intact, got %#v", state.Workspaces[1])
	}
}

func TestLoadWorkspaceStateCmdRestoresWorkspaceSetAndSwitchesBetweenThem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, "workspace-state.json")
	if err := os.WriteFile(statePath, []byte(`{
  "version": 1,
  "active_workspace": 1,
  "workspaces": [
    {
      "name": "main",
      "active_tab": 0,
      "tabs": [
        {
          "name": "primary",
          "active_pane_id": "pane-1",
          "floating_visible": true,
          "root": {"pane_id": "pane-1"},
          "panes": [
            {
              "id": "pane-1",
              "title": "primary-pane",
              "terminal_id": "term-001",
              "command": ["vim", "."],
              "terminal_state": "running",
              "mode": "fit"
            }
          ]
        }
      ]
    },
    {
      "name": "workspace-2",
      "active_tab": 0,
      "tabs": [
        {
          "name": "secondary",
          "active_pane_id": "pane-2",
          "floating_visible": true,
          "root": {"pane_id": "pane-2"},
          "panes": [
            {
              "id": "pane-2",
              "title": "secondary-pane",
              "terminal_id": "term-001",
              "command": ["tail", "-f", "worker.log"],
              "terminal_state": "running",
              "mode": "fit"
            }
          ]
        }
      ]
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "term-001",
				Name:    "shared",
				Command: []string{"tail", "-f", "worker.log"},
				State:   "running",
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.loadWorkspaceStateCmd(statePath))
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.workspace.Name != "workspace-2" {
		t.Fatalf("expected active workspace workspace-2, got %q", model.workspace.Name)
	}
	if len(model.workspaceOrder) != 2 {
		t.Fatalf("expected two restored workspaces, got %v", model.workspaceOrder)
	}

	openWorkspacePickerAndSelect(t, model, "main")
	if model.workspace.Name != "main" {
		t.Fatalf("expected switch to restored main workspace, got %q", model.workspace.Name)
	}
	if pane := model.currentTab().Panes[model.currentTab().ActivePaneID]; pane == nil || pane.TerminalID != "term-001" {
		t.Fatalf("expected restored main workspace pane, got %#v", pane)
	}
}

func TestLoadWorkspaceStateCmdRestoresActiveTabAutoAcquireResize(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	statePath := filepath.Join(home, "workspace-state.json")
	if err := os.WriteFile(statePath, []byte(`{
  "version": 1,
  "workspace": {
    "name": "restore-ws",
    "active_tab": 1,
    "tabs": [
      {
        "name": "left",
        "active_pane_id": "pane-1",
        "floating_visible": false,
        "root": {"pane_id": "pane-1"},
        "panes": [
          {
            "id": "pane-1",
            "title": "shared-left",
            "terminal_id": "term-001",
            "command": ["bash"],
            "terminal_state": "running",
            "mode": "fit"
          }
        ]
      },
      {
        "name": "right",
        "active_pane_id": "pane-2",
        "floating_visible": false,
        "auto_acquire_resize": true,
        "root": {"pane_id": "pane-2"},
        "panes": [
          {
            "id": "pane-2",
            "title": "shared-right",
            "terminal_id": "term-001",
            "command": ["bash"],
            "terminal_state": "running",
            "mode": "fit"
          }
        ]
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-001", Name: "shared", Command: []string{"bash"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.loadWorkspaceStateCmd(statePath))
	_, cmd := model.Update(msg)
	runAllCmds(t, cmd)

	tab := model.currentTab()
	if tab == nil || !tab.AutoAcquireResize {
		t.Fatalf("expected restored active tab auto-acquire, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || !pane.ResizeAcquired {
		t.Fatalf("expected restored active pane to auto-acquire resize, got %#v", pane)
	}
	if client.resizeCalls == 0 {
		t.Fatal("expected workspace restore to trigger resize after auto-acquire")
	}
}

func TestWorkspaceSwitchRestoresAutoAcquireResizeOnActivatedWorkspace(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-001", Name: "shared", Command: []string{"bash"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})
	model.width = 120
	model.height = 30

	model.workspace = Workspace{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*Tab{
			{
				Name:         "main-tab",
				ActivePaneID: "pane-main",
				Root:         NewLeaf("pane-main"),
				Panes: map[string]*Pane{
					"pane-main": {
						ID:    "pane-main",
						Title: "main",
						Viewport: &Viewport{
							TerminalID:    "term-main",
							Command:       []string{"bash"},
							TerminalState: "running",
							Mode:          ViewportModeFit,
						},
					},
				},
			},
		},
	}
	model.workspaceStore = map[string]Workspace{
		"main": model.workspace,
		"shared": {
			Name:      "shared",
			ActiveTab: 1,
			Tabs: []*Tab{
				{
					Name:         "left",
					ActivePaneID: "pane-1",
					Root:         NewLeaf("pane-1"),
					Panes: map[string]*Pane{
						"pane-1": {
							ID:    "pane-1",
							Title: "left",
							Viewport: &Viewport{
								TerminalID:    "term-001",
								Command:       []string{"bash"},
								TerminalState: "running",
								Mode:          ViewportModeFit,
							},
						},
					},
				},
				{
					Name:              "right",
					ActivePaneID:      "pane-2",
					AutoAcquireResize: true,
					Root:              NewLeaf("pane-2"),
					Panes: map[string]*Pane{
						"pane-2": {
							ID:    "pane-2",
							Title: "right",
							Viewport: &Viewport{
								TerminalID:    "term-001",
								Command:       []string{"bash"},
								TerminalState: "running",
								Mode:          ViewportModeFit,
							},
						},
					},
				},
			},
		},
	}
	model.workspaceOrder = []string{"main", "shared"}
	model.activeWorkspace = 0

	msg := mustRunCmd(t, model.switchWorkspaceCmd("shared"))
	_, cmd := model.Update(msg)
	runAllCmds(t, cmd)

	tab := model.currentTab()
	if model.workspace.Name != "shared" {
		t.Fatalf("expected to switch to shared workspace, got %q", model.workspace.Name)
	}
	if tab == nil || !tab.AutoAcquireResize {
		t.Fatalf("expected switched workspace tab auto-acquire, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || !pane.ResizeAcquired {
		t.Fatalf("expected switched workspace active pane to auto-acquire resize, got %#v", pane)
	}
	if client.resizeCalls == 0 {
		t.Fatal("expected workspace switch to trigger resize after auto-acquire")
	}
}
