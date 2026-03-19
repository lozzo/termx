package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
)

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
