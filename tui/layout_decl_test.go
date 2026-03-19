package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
)

func TestParseLayoutYAMLParsesSplitTreeAndViewportModes(t *testing.T) {
	data := []byte(`
name: dev
tabs:
  - name: coding
    tiling:
      split: horizontal
      ratio: 0.6
      children:
        - terminal:
            tag: "role=editor,project=api"
            _hint_id: "term-001"
            command: "vim ."
            cwd: ~/project
            mode: fixed
        - split: vertical
          children:
            - terminal:
                command: "make watch"
            - terminal:
                tag: "role=log"
    floating:
      - terminal:
          tag: "role=ai-agent"
          command: claude-code
        width: 80
        height: 24
`)

	layout, err := ParseLayoutYAML(data)
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}
	if layout.Name != "dev" {
		t.Fatalf("expected layout name dev, got %q", layout.Name)
	}
	if len(layout.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(layout.Tabs))
	}
	tab := layout.Tabs[0]
	if tab.Name != "coding" {
		t.Fatalf("expected tab name coding, got %q", tab.Name)
	}
	if tab.Tiling == nil || tab.Tiling.Split != SplitHorizontal {
		t.Fatalf("expected horizontal split root, got %#v", tab.Tiling)
	}
	if got := len(tab.Tiling.Children); got != 2 {
		t.Fatalf("expected 2 root children, got %d", got)
	}
	left := tab.Tiling.Children[0]
	if left.Terminal == nil {
		t.Fatal("expected left child terminal")
	}
	if left.Terminal.Tag != "role=editor,project=api" {
		t.Fatalf("unexpected tag expression %q", left.Terminal.Tag)
	}
	if left.Terminal.HintID != "term-001" {
		t.Fatalf("unexpected hint id %q", left.Terminal.HintID)
	}
	if left.Terminal.Mode != ViewportModeFixed {
		t.Fatalf("expected fixed mode, got %q", left.Terminal.Mode)
	}
	if got := len(tab.Floating); got != 1 {
		t.Fatalf("expected 1 floating entry, got %d", got)
	}
	if tab.Floating[0].Terminal == nil || tab.Floating[0].Terminal.Mode != ViewportModeFixed {
		t.Fatalf("expected floating terminal to default to fixed mode, got %#v", tab.Floating[0].Terminal)
	}
}

func TestParseLayoutYAMLRejectsInvalidShapes(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{
			name: "split without two children",
			data: `
name: broken
tabs:
  - name: only
    tiling:
      split: horizontal
      children:
        - terminal:
            command: "echo hi"
`,
		},
		{
			name: "terminal and split together",
			data: `
name: broken
tabs:
  - name: only
    tiling:
      split: vertical
      children:
        - terminal:
            command: "echo hi"
        - terminal:
            command: "echo bye"
      terminal:
        command: "oops"
`,
		},
		{
			name: "missing tiling",
			data: `
name: broken
tabs:
  - name: only
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseLayoutYAML([]byte(tc.data)); err == nil {
				t.Fatal("expected layout parser to reject invalid shape")
			}
		})
	}
}

func TestSelectTerminalForLayoutUsesHintThenStableTagOrdering(t *testing.T) {
	now := time.Now()
	used := map[string]struct{}{}
	terminals := []protocol.TerminalInfo{
		{
			ID:        "term-003",
			State:     "exited",
			CreatedAt: now.Add(-3 * time.Hour),
			Tags: map[string]string{
				"role":    "editor",
				"project": "api",
			},
		},
		{
			ID:        "term-002",
			State:     "running",
			CreatedAt: now.Add(-1 * time.Hour),
			Tags: map[string]string{
				"role":    "editor",
				"project": "api",
			},
		},
		{
			ID:        "term-001",
			State:     "running",
			CreatedAt: now.Add(-2 * time.Hour),
			Tags: map[string]string{
				"role":    "editor",
				"project": "api",
			},
		},
	}

	decl := LayoutTerminalSpec{
		HintID: "term-002",
		Tag:    "role=editor,project=api",
	}
	selected := SelectTerminalForLayout(terminals, decl, used)
	if selected == nil || selected.ID != "term-002" {
		t.Fatalf("expected hint to win, got %#v", selected)
	}
	used[selected.ID] = struct{}{}

	decl.HintID = "missing"
	selected = SelectTerminalForLayout(terminals, decl, used)
	if selected == nil || selected.ID != "term-001" {
		t.Fatalf("expected stable ordering to pick oldest running unused terminal, got %#v", selected)
	}
}

func TestSelectTerminalForLayoutSupportsKeyOnlyTagsAndAvoidsReusingUsedTerminal(t *testing.T) {
	now := time.Now()
	used := map[string]struct{}{"term-001": {}}
	terminals := []protocol.TerminalInfo{
		{
			ID:        "term-001",
			State:     "running",
			CreatedAt: now.Add(-2 * time.Hour),
			Tags: map[string]string{
				"role": "editor",
			},
		},
		{
			ID:        "term-002",
			State:     "running",
			CreatedAt: now.Add(-1 * time.Hour),
			Tags: map[string]string{
				"role": "build",
			},
		},
	}

	selected := SelectTerminalForLayout(terminals, LayoutTerminalSpec{Tag: "role"}, used)
	if selected == nil || selected.ID != "term-002" {
		t.Fatalf("expected key-only tag match to choose next unused terminal, got %#v", selected)
	}
}

func TestExportLayoutYAMLRoundTripsCurrentWorkspace(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "dev"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	createSplitPaneViaPicker(t, model, SplitVertical)

	tab := model.currentTab()
	ids := tab.Root.LeafIDs()
	left := tab.Panes[ids[0]]
	right := tab.Panes[ids[1]]
	left.Tags = map[string]string{"role": "shell"}
	left.Command = []string{"zsh"}
	right.Tags = map[string]string{"role": "build", "project": "api"}
	right.Command = []string{"make", "watch"}
	right.Mode = ViewportModeFixed

	data, err := ExportLayoutYAML("dev-layout", &model.workspace)
	if err != nil {
		t.Fatalf("ExportLayoutYAML returned error: %v", err)
	}

	layout, err := ParseLayoutYAML(data)
	if err != nil {
		t.Fatalf("expected exported YAML to parse, got error: %v", err)
	}
	if layout.Name != "dev-layout" {
		t.Fatalf("expected exported layout name, got %q", layout.Name)
	}
	if len(layout.Tabs) != 1 {
		t.Fatalf("expected 1 tab in exported layout, got %d", len(layout.Tabs))
	}
	root := layout.Tabs[0].Tiling
	if root == nil || root.Split != SplitVertical {
		t.Fatalf("expected exported split root, got %#v", root)
	}
	if got := len(root.Children); got != 2 {
		t.Fatalf("expected exported split children, got %d", got)
	}
	if root.Children[0].Terminal == nil || root.Children[0].Terminal.Tag != "role=shell" {
		t.Fatalf("expected left terminal tags to round-trip, got %#v", root.Children[0].Terminal)
	}
	if root.Children[1].Terminal == nil || root.Children[1].Terminal.Mode != ViewportModeFixed {
		t.Fatalf("expected right terminal mode to round-trip, got %#v", root.Children[1].Terminal)
	}
}

func TestExportLayoutSpecIncludesFloatingEntries(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "dev"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	createFloatingPaneViaPicker(t, model)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane, got %#v", tab)
	}
	floatPane := tab.Panes[tab.Floating[0].PaneID]
	if floatPane == nil {
		t.Fatal("expected floating pane")
	}
	floatPane.Tags = map[string]string{"role": "agent"}
	floatPane.Command = []string{"claude-code"}
	tab.Floating[0].Rect = Rect{X: 12, Y: 5, W: 46, H: 14}

	spec, err := ExportLayoutSpec("dev-layout", &model.workspace)
	if err != nil {
		t.Fatalf("ExportLayoutSpec returned error: %v", err)
	}
	if len(spec.Tabs) != 1 {
		t.Fatalf("expected 1 tab in exported layout, got %d", len(spec.Tabs))
	}
	if len(spec.Tabs[0].Floating) != 1 {
		t.Fatalf("expected 1 floating entry in exported layout, got %d", len(spec.Tabs[0].Floating))
	}
	entry := spec.Tabs[0].Floating[0]
	if entry.Terminal == nil {
		t.Fatal("expected floating terminal spec")
	}
	if entry.Terminal.Tag != "role=agent" {
		t.Fatalf("expected floating tags to round-trip, got %#v", entry.Terminal)
	}
	if entry.Terminal.Command != "claude-code" {
		t.Fatalf("expected floating command to round-trip, got %#v", entry.Terminal)
	}
	if entry.Width != 46 || entry.Height != 14 {
		t.Fatalf("expected floating size to round-trip, got %+v", entry)
	}
}

func TestBuildWorkspaceFromLayoutSpecIncludesFloatingPanes(t *testing.T) {
	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: coding
    tiling:
      terminal:
        tag: "role=shell"
        command: "zsh"
    floating:
      - terminal:
          tag: "role=agent"
          command: "claude-code"
        width: 72
        height: 18
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	workspace, plans, err := BuildWorkspaceFromLayoutSpec(layout, "loaded-dev", []protocol.TerminalInfo{
		{
			ID:      "term-shell",
			Name:    "shell",
			Command: []string{"zsh"},
			State:   "running",
			Tags:    map[string]string{"role": "shell"},
		},
	}, LayoutResolveCreate)
	if err != nil {
		t.Fatalf("BuildWorkspaceFromLayoutSpec returned error: %v", err)
	}
	tab := workspace.Tabs[0]
	if len(tab.Floating) != 1 {
		t.Fatalf("expected 1 floating entry, got %d", len(tab.Floating))
	}
	floatPane := tab.Panes[tab.Floating[0].PaneID]
	if floatPane == nil {
		t.Fatal("expected floating pane to exist")
	}
	if floatPane.Mode != ViewportModeFixed {
		t.Fatalf("expected floating pane to default to fixed mode, got %q", floatPane.Mode)
	}
	if floatPane.TerminalState != "waiting" {
		t.Fatalf("expected unmatched floating pane to be waiting, got %q", floatPane.TerminalState)
	}
	if len(plans) != 1 || plans[0].Terminal.Command != "claude-code" {
		t.Fatalf("expected one floating create plan, got %#v", plans)
	}
}

func TestBuildWorkspaceFromLayoutSpecArrangeGridBuildsPanesForAllMatches(t *testing.T) {
	layout, err := ParseLayoutYAML([]byte(`
name: monitoring
tabs:
  - name: logs
    tiling:
      arrange: grid
      match:
        tag: "type=log"
      min_size: [40, 10]
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	now := time.Now()
	workspace, plans, err := BuildWorkspaceFromLayoutSpec(layout, "", []protocol.TerminalInfo{
		{ID: "term-c", Name: "redis.log", Command: []string{"tail", "-f", "redis.log"}, State: "exited", CreatedAt: now.Add(-3 * time.Hour), Tags: map[string]string{"type": "log"}},
		{ID: "term-b", Name: "worker.log", Command: []string{"tail", "-f", "worker.log"}, State: "running", CreatedAt: now.Add(-2 * time.Hour), Tags: map[string]string{"type": "log"}},
		{ID: "term-a", Name: "api.log", Command: []string{"tail", "-f", "api.log"}, State: "running", CreatedAt: now.Add(-4 * time.Hour), Tags: map[string]string{"type": "log"}},
		{ID: "term-d", Name: "shell", Command: []string{"zsh"}, State: "running", CreatedAt: now.Add(-1 * time.Hour), Tags: map[string]string{"role": "shell"}},
	}, LayoutResolveCreate)
	if err != nil {
		t.Fatalf("BuildWorkspaceFromLayoutSpec returned error: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("expected arrange grid not to produce create plans for existing matches, got %#v", plans)
	}
	tab := workspace.Tabs[0]
	if tab.Root == nil {
		t.Fatal("expected arrange grid root")
	}
	ids := tab.Root.LeafIDs()
	if len(ids) != 3 {
		t.Fatalf("expected 3 arranged panes, got %v", ids)
	}
	got := make([]string, 0, len(ids))
	for _, paneID := range ids {
		got = append(got, tab.Panes[paneID].TerminalID)
	}
	want := []string{"term-a", "term-b", "term-c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected arranged terminal order %v, got %v", want, got)
	}
}

func TestBuildWorkspaceFromLayoutSpecArrangeHorizontalUsesEvenHorizontalTree(t *testing.T) {
	layout, err := ParseLayoutYAML([]byte(`
name: monitoring
tabs:
  - name: logs
    tiling:
      arrange: horizontal
      match:
        tag: "type=log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	workspace, _, err := BuildWorkspaceFromLayoutSpec(layout, "", []protocol.TerminalInfo{
		{ID: "term-a", Name: "a", Command: []string{"tail"}, State: "running", Tags: map[string]string{"type": "log"}},
		{ID: "term-b", Name: "b", Command: []string{"tail"}, State: "running", Tags: map[string]string{"type": "log"}},
	}, LayoutResolveCreate)
	if err != nil {
		t.Fatalf("BuildWorkspaceFromLayoutSpec returned error: %v", err)
	}
	if workspace.Tabs[0].Root == nil || workspace.Tabs[0].Root.Direction != SplitHorizontal {
		t.Fatalf("expected horizontal arrange root, got %#v", workspace.Tabs[0].Root)
	}
}

func TestBuildWorkspaceFromLayoutSpecArrangeWithoutMatchesLeavesWaitingPane(t *testing.T) {
	layout, err := ParseLayoutYAML([]byte(`
name: monitoring
tabs:
  - name: logs
    tiling:
      arrange: grid
      match:
        tag: "type=log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	workspace, plans, err := BuildWorkspaceFromLayoutSpec(layout, "", nil, LayoutResolveCreate)
	if err != nil {
		t.Fatalf("BuildWorkspaceFromLayoutSpec returned error: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("expected no create plan when arrange has no command source, got %#v", plans)
	}
	tab := workspace.Tabs[0]
	if len(tab.Panes) != 1 {
		t.Fatalf("expected one waiting pane fallback, got %d", len(tab.Panes))
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalState != "waiting" {
		t.Fatalf("expected waiting pane fallback, got %#v", pane)
	}
}

func TestBuildWorkspaceFromLayoutSpecReusesMatchesAndCollectsCreatePlans(t *testing.T) {
	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: coding
    tiling:
      split: vertical
      children:
        - terminal:
            tag: "role=editor,project=api"
            command: "vim ."
        - terminal:
            tag: "role=build"
            command: "make watch"
            mode: fixed
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	now := time.Now()
	workspace, plans, err := BuildWorkspaceFromLayoutSpec(layout, "loaded-dev", []protocol.TerminalInfo{
		{
			ID:        "term-editor",
			Name:      "editor",
			Command:   []string{"vim", "."},
			State:     "running",
			CreatedAt: now.Add(-2 * time.Hour),
			Tags: map[string]string{
				"role":    "editor",
				"project": "api",
			},
		},
	}, LayoutResolveCreate)
	if err != nil {
		t.Fatalf("BuildWorkspaceFromLayoutSpec returned error: %v", err)
	}
	if workspace.Name != "loaded-dev" {
		t.Fatalf("expected workspace name loaded-dev, got %q", workspace.Name)
	}
	if len(workspace.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(workspace.Tabs))
	}
	tab := workspace.Tabs[0]
	if tab.Root == nil || tab.Root.Direction != SplitVertical {
		t.Fatalf("expected vertical split root, got %#v", tab.Root)
	}
	if len(tab.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(tab.Panes))
	}

	var matched, waiting *Pane
	for _, pane := range tab.Panes {
		switch pane.TerminalID {
		case "term-editor":
			matched = pane
		default:
			waiting = pane
		}
	}
	if matched == nil {
		t.Fatal("expected matched editor pane")
	}
	if matched.TerminalState != "running" {
		t.Fatalf("expected matched pane running, got %q", matched.TerminalState)
	}
	if waiting == nil || waiting.TerminalState != "waiting" {
		t.Fatalf("expected unmatched pane to stay waiting, got %#v", waiting)
	}
	if waiting.Mode != ViewportModeFixed {
		t.Fatalf("expected unmatched pane mode to come from layout, got %q", waiting.Mode)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 create plan, got %d", len(plans))
	}
	if plans[0].TabName != "coding" || plans[0].Terminal.Command != "make watch" {
		t.Fatalf("unexpected create plan %#v", plans[0])
	}
}

func TestBuildWorkspaceFromLayoutSpecSkipPolicyLeavesWaitingViewport(t *testing.T) {
	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: scratch
    tiling:
      terminal:
        tag: "role=log"
        command: "tail -f app.log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	workspace, plans, err := BuildWorkspaceFromLayoutSpec(layout, "", nil, LayoutResolveSkip)
	if err != nil {
		t.Fatalf("BuildWorkspaceFromLayoutSpec returned error: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("expected skip policy to avoid create plans, got %d", len(plans))
	}
	tab := workspace.Tabs[0]
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active waiting pane")
	}
	if pane.TerminalState != "waiting" {
		t.Fatalf("expected waiting terminal state, got %q", pane.TerminalState)
	}
	if got := strings.Join(welcomePaneLines(pane), "\n"); !strings.Contains(got, "waiting for terminal") {
		t.Fatalf("expected waiting placeholder text, got:\n%s", got)
	}
}
