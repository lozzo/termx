package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lozzow/termx/protocol"
	"go.yaml.in/yaml/v3"
)

type LayoutSpec struct {
	Name string          `yaml:"name"`
	Tabs []LayoutTabSpec `yaml:"tabs"`
}

type LayoutTabSpec struct {
	Name     string              `yaml:"name"`
	Tiling   *LayoutNodeSpec     `yaml:"tiling"`
	Floating []FloatingEntrySpec `yaml:"floating,omitempty"`
}

type FloatingEntrySpec struct {
	Terminal *LayoutTerminalSpec `yaml:"terminal"`
	Width    int                 `yaml:"width,omitempty"`
	Height   int                 `yaml:"height,omitempty"`
	Position string              `yaml:"position,omitempty"`
}

type LayoutNodeSpec struct {
	Split    SplitDirection      `yaml:"split,omitempty"`
	Ratio    float64             `yaml:"ratio,omitempty"`
	Children []LayoutNodeSpec    `yaml:"children,omitempty"`
	Terminal *LayoutTerminalSpec `yaml:"terminal,omitempty"`
	Arrange  string              `yaml:"arrange,omitempty"`
	Match    *LayoutMatchSpec    `yaml:"match,omitempty"`
	MinSize  []int               `yaml:"min_size,omitempty"`
}

type LayoutTerminalSpec struct {
	Tag     string            `yaml:"tag,omitempty"`
	HintID  string            `yaml:"_hint_id,omitempty"`
	Command string            `yaml:"command,omitempty"`
	Cwd     string            `yaml:"cwd,omitempty"`
	Mode    ViewportMode      `yaml:"mode,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

type LayoutMatchSpec struct {
	Tag string `yaml:"tag,omitempty"`
}

type LayoutResolvePolicy string

const (
	LayoutResolveCreate LayoutResolvePolicy = "create"
	LayoutResolvePrompt LayoutResolvePolicy = "prompt"
	LayoutResolveSkip   LayoutResolvePolicy = "skip"
)

type LayoutCreatePlan struct {
	TabName  string
	PaneID   string
	Terminal LayoutTerminalSpec
}

func ParseLayoutYAML(data []byte) (*LayoutSpec, error) {
	var layout LayoutSpec
	if err := yaml.Unmarshal(data, &layout); err != nil {
		return nil, err
	}
	if err := validateLayoutSpec(&layout); err != nil {
		return nil, err
	}
	return &layout, nil
}

func BuildWorkspaceFromLayoutSpec(layout *LayoutSpec, workspaceName string, terminals []protocol.TerminalInfo, policy LayoutResolvePolicy) (*Workspace, []LayoutCreatePlan, error) {
	if err := validateLayoutSpec(layout); err != nil {
		return nil, nil, err
	}
	if workspaceName == "" {
		workspaceName = layout.Name
	}
	if policy == "" {
		policy = LayoutResolveCreate
	}

	builder := layoutWorkspaceBuilder{
		workspace: Workspace{Name: workspaceName},
		terminals: terminals,
		used:      map[string]struct{}{},
		policy:    policy,
	}
	for _, tabSpec := range layout.Tabs {
		tab, plans, err := builder.buildTab(tabSpec)
		if err != nil {
			return nil, nil, err
		}
		builder.workspace.Tabs = append(builder.workspace.Tabs, tab)
		builder.plans = append(builder.plans, plans...)
	}
	if len(builder.workspace.Tabs) == 0 {
		return nil, nil, fmt.Errorf("layout produced no tabs")
	}
	return &builder.workspace, builder.plans, nil
}

func ExportLayoutYAML(name string, workspace *Workspace) ([]byte, error) {
	spec, err := ExportLayoutSpec(name, workspace)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(spec)
}

func ExportLayoutSpec(name string, workspace *Workspace) (*LayoutSpec, error) {
	if workspace == nil {
		return nil, fmt.Errorf("workspace is nil")
	}
	if strings.TrimSpace(name) == "" {
		name = workspace.Name
	}
	spec := &LayoutSpec{Name: name}
	for _, tab := range workspace.Tabs {
		if tab == nil {
			continue
		}
		if tab.Root == nil {
			return nil, fmt.Errorf("tab %q has no tiling root", tab.Name)
		}
		tiling, err := exportLayoutNodeSpec(tab.Root, tab.Panes)
		if err != nil {
			return nil, fmt.Errorf("tab %q: %w", tab.Name, err)
		}
		spec.Tabs = append(spec.Tabs, LayoutTabSpec{
			Name:     tab.Name,
			Tiling:   tiling,
			Floating: exportFloatingEntrySpecs(tab, tab.Panes),
		})
	}
	if len(spec.Tabs) == 0 {
		return nil, fmt.Errorf("workspace has no tabs")
	}
	return spec, nil
}

func validateLayoutSpec(layout *LayoutSpec) error {
	if layout == nil {
		return fmt.Errorf("layout is nil")
	}
	if strings.TrimSpace(layout.Name) == "" {
		return fmt.Errorf("layout name is required")
	}
	if len(layout.Tabs) == 0 {
		return fmt.Errorf("layout must define at least one tab")
	}
	for i := range layout.Tabs {
		tab := &layout.Tabs[i]
		if strings.TrimSpace(tab.Name) == "" {
			return fmt.Errorf("tab %d name is required", i)
		}
		if tab.Tiling == nil {
			return fmt.Errorf("tab %q tiling is required", tab.Name)
		}
		if err := validateLayoutNodeSpec(tab.Tiling); err != nil {
			return fmt.Errorf("tab %q: %w", tab.Name, err)
		}
		for j := range tab.Floating {
			entry := &tab.Floating[j]
			if entry.Terminal == nil {
				return fmt.Errorf("tab %q floating entry %d terminal is required", tab.Name, j)
			}
			normalizeTerminalMode(entry.Terminal, ViewportModeFixed)
		}
	}
	return nil
}

func validateLayoutNodeSpec(node *LayoutNodeSpec) error {
	if node == nil {
		return fmt.Errorf("layout node is nil")
	}
	kinds := 0
	if node.Terminal != nil {
		kinds++
	}
	if node.Split != "" {
		kinds++
	}
	if node.Arrange != "" {
		kinds++
	}
	if kinds != 1 {
		return fmt.Errorf("layout node must define exactly one of terminal, split, or arrange")
	}

	switch {
	case node.Terminal != nil:
		normalizeTerminalMode(node.Terminal, ViewportModeFit)
		return nil
	case node.Split != "":
		if node.Split != SplitHorizontal && node.Split != SplitVertical {
			return fmt.Errorf("unsupported split direction %q", node.Split)
		}
		if len(node.Children) != 2 {
			return fmt.Errorf("split node must have exactly two children")
		}
		if node.Ratio == 0 {
			node.Ratio = 0.5
		}
		if node.Ratio <= 0 || node.Ratio >= 1 {
			return fmt.Errorf("split ratio must be between 0 and 1")
		}
		for i := range node.Children {
			if err := validateLayoutNodeSpec(&node.Children[i]); err != nil {
				return err
			}
		}
		return nil
	case node.Arrange != "":
		if node.Match == nil || strings.TrimSpace(node.Match.Tag) == "" {
			return fmt.Errorf("arrange node requires match.tag")
		}
		switch node.Arrange {
		case "grid", "horizontal", "vertical":
		default:
			return fmt.Errorf("unsupported arrange mode %q", node.Arrange)
		}
		if len(node.MinSize) > 0 && len(node.MinSize) != 2 {
			return fmt.Errorf("arrange min_size must contain exactly two integers")
		}
		return nil
	default:
		return fmt.Errorf("unsupported layout node")
	}
}

func normalizeTerminalMode(terminal *LayoutTerminalSpec, fallback ViewportMode) {
	if terminal == nil || terminal.Mode != "" {
		return
	}
	terminal.Mode = fallback
}

func SelectTerminalForLayout(terminals []protocol.TerminalInfo, decl LayoutTerminalSpec, used map[string]struct{}) *protocol.TerminalInfo {
	if decl.HintID != "" {
		for i := range terminals {
			if terminals[i].ID == decl.HintID {
				if _, ok := used[terminals[i].ID]; ok {
					break
				}
				return &terminals[i]
			}
		}
	}

	candidates := make([]protocol.TerminalInfo, 0, len(terminals))
	for _, terminal := range terminals {
		if _, ok := used[terminal.ID]; ok {
			continue
		}
		if !terminalMatchesTagExpr(terminal, decl.Tag) {
			continue
		}
		candidates = append(candidates, terminal)
	}
	slices.SortStableFunc(candidates, compareLayoutTerminalCandidates)
	if len(candidates) == 0 {
		return nil
	}
	selected := candidates[0]
	return &selected
}

func compareLayoutTerminalCandidates(a, b protocol.TerminalInfo) int {
	aRunning := a.State == "running"
	bRunning := b.State == "running"
	if aRunning != bRunning {
		if aRunning {
			return -1
		}
		return 1
	}
	if a.CreatedAt.Before(b.CreatedAt) {
		return -1
	}
	if b.CreatedAt.Before(a.CreatedAt) {
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}

func terminalMatchesTagExpr(info protocol.TerminalInfo, expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if key, value, ok := strings.Cut(part, "="); ok {
			if info.Tags[strings.TrimSpace(key)] != strings.TrimSpace(value) {
				return false
			}
			continue
		}
		if _, ok := info.Tags[part]; !ok {
			return false
		}
	}
	return true
}

type layoutWorkspaceBuilder struct {
	workspace Workspace
	terminals []protocol.TerminalInfo
	used      map[string]struct{}
	policy    LayoutResolvePolicy
	nextPane  int
	plans     []LayoutCreatePlan
}

func (b *layoutWorkspaceBuilder) buildTab(spec LayoutTabSpec) (*Tab, []LayoutCreatePlan, error) {
	tab := newTab(spec.Name)
	root, err := b.buildNode(spec.Tiling, tab, spec.Name)
	if err != nil {
		return nil, nil, err
	}
	tab.Root = root
	for i := range spec.Floating {
		entry := spec.Floating[i]
		pane := b.buildPaneForTerminal(*entry.Terminal, spec.Name)
		tab.Panes[pane.ID] = pane
		tab.Floating = append(tab.Floating, &FloatingPane{
			PaneID: pane.ID,
			Rect: Rect{
				W: max(8, entry.Width),
				H: max(4, entry.Height),
			},
			Z: len(tab.Floating),
		})
	}
	tab.ActivePaneID = firstPaneID(tab.Panes)
	plans := append([]LayoutCreatePlan(nil), b.plans...)
	b.plans = b.plans[:0]
	return tab, plans, nil
}

func (b *layoutWorkspaceBuilder) buildNode(spec *LayoutNodeSpec, tab *Tab, tabName string) (*LayoutNode, error) {
	if spec == nil {
		return nil, fmt.Errorf("layout node is nil")
	}
	if spec.Terminal != nil {
		pane := b.buildPaneForTerminal(*spec.Terminal, tabName)
		tab.Panes[pane.ID] = pane
		return NewLeaf(pane.ID), nil
	}
	if spec.Split != "" {
		first, err := b.buildNode(&spec.Children[0], tab, tabName)
		if err != nil {
			return nil, err
		}
		second, err := b.buildNode(&spec.Children[1], tab, tabName)
		if err != nil {
			return nil, err
		}
		return &LayoutNode{
			Direction: spec.Split,
			Ratio:     spec.Ratio,
			First:     first,
			Second:    second,
		}, nil
	}
	if spec.Arrange != "" {
		return b.buildArrangeNode(spec, tab, tabName)
	}
	return nil, fmt.Errorf("unsupported layout node")
}

func (b *layoutWorkspaceBuilder) buildArrangeNode(spec *LayoutNodeSpec, tab *Tab, tabName string) (*LayoutNode, error) {
	matches := selectTerminalsForArrange(b.terminals, spec.Match.Tag, b.used)
	if len(matches) == 0 {
		pane := b.buildWaitingArrangePane(spec.Match.Tag)
		tab.Panes[pane.ID] = pane
		return NewLeaf(pane.ID), nil
	}

	ids := make([]string, 0, len(matches))
	for _, info := range matches {
		pane := b.buildPaneForExistingTerminal(info)
		tab.Panes[pane.ID] = pane
		ids = append(ids, pane.ID)
	}

	switch spec.Arrange {
	case "horizontal":
		return buildEvenLayout(ids, SplitHorizontal), nil
	case "vertical":
		return buildEvenLayout(ids, SplitVertical), nil
	case "grid":
		return buildTiledLayout(ids, SplitVertical), nil
	default:
		return nil, fmt.Errorf("unsupported arrange mode %q", spec.Arrange)
	}
}

func (b *layoutWorkspaceBuilder) buildPaneForTerminal(spec LayoutTerminalSpec, tabName string) *Pane {
	paneID := b.nextPaneID()
	selected := SelectTerminalForLayout(b.terminals, spec, b.used)
	if selected != nil {
		pane := b.buildPaneForExistingTerminal(*selected)
		pane.Mode = defaultViewportMode(spec.Mode, ViewportModeFit)
		return pane
	}

	pane := &Pane{
		ID:    paneID,
		Title: paneTitleForCommand("", spec.Command, "waiting"),
		Viewport: &Viewport{
			Command:       commandStringToSlice(spec.Command),
			Tags:          parseTagExpressionMap(spec.Tag),
			TerminalState: "waiting",
			Mode:          defaultViewportMode(spec.Mode, ViewportModeFit),
			renderDirty:   true,
		},
	}
	if b.policy == LayoutResolveCreate || b.policy == LayoutResolvePrompt {
		b.plans = append(b.plans, LayoutCreatePlan{
			TabName:  tabName,
			PaneID:   paneID,
			Terminal: spec,
		})
	}
	return pane
}

func (b *layoutWorkspaceBuilder) buildPaneForExistingTerminal(info protocol.TerminalInfo) *Pane {
	paneID := b.nextPaneID()
	b.used[info.ID] = struct{}{}
	return &Pane{
		ID:    paneID,
		Title: paneTitleForTerminal(info),
		Viewport: &Viewport{
			TerminalID:    info.ID,
			Name:          info.Name,
			Command:       append([]string(nil), info.Command...),
			Tags:          cloneStringMap(info.Tags),
			TerminalState: defaultTerminalState(info.State),
			ExitCode:      info.ExitCode,
			Mode:          ViewportModeFit,
			renderDirty:   true,
		},
	}
}

func (b *layoutWorkspaceBuilder) buildWaitingArrangePane(tagExpr string) *Pane {
	paneID := b.nextPaneID()
	return &Pane{
		ID:    paneID,
		Title: "waiting",
		Viewport: &Viewport{
			Tags:          parseTagExpressionMap(tagExpr),
			TerminalState: "waiting",
			Mode:          ViewportModeFit,
			renderDirty:   true,
		},
	}
}

func selectTerminalsForArrange(terminals []protocol.TerminalInfo, expr string, used map[string]struct{}) []protocol.TerminalInfo {
	matches := make([]protocol.TerminalInfo, 0, len(terminals))
	for _, terminal := range terminals {
		if _, ok := used[terminal.ID]; ok {
			continue
		}
		if !terminalMatchesTagExpr(terminal, expr) {
			continue
		}
		matches = append(matches, terminal)
	}
	slices.SortStableFunc(matches, compareLayoutTerminalCandidates)
	return matches
}

func (b *layoutWorkspaceBuilder) nextPaneID() string {
	b.nextPane++
	return fmt.Sprintf("layout-pane-%03d", b.nextPane)
}

func exportLayoutNodeSpec(node *LayoutNode, panes map[string]*Pane) (*LayoutNodeSpec, error) {
	if node == nil {
		return nil, fmt.Errorf("layout node is nil")
	}
	if node.IsLeaf() {
		pane := panes[node.PaneID]
		if pane == nil {
			return nil, fmt.Errorf("missing pane %q", node.PaneID)
		}
		return &LayoutNodeSpec{
			Terminal: exportTerminalSpec(pane),
		}, nil
	}
	first, err := exportLayoutNodeSpec(node.First, panes)
	if err != nil {
		return nil, err
	}
	second, err := exportLayoutNodeSpec(node.Second, panes)
	if err != nil {
		return nil, err
	}
	return &LayoutNodeSpec{
		Split:    node.Direction,
		Ratio:    node.Ratio,
		Children: []LayoutNodeSpec{*first, *second},
	}, nil
}

func exportTerminalSpec(pane *Pane) *LayoutTerminalSpec {
	spec := &LayoutTerminalSpec{
		Command: strings.Join(pane.Command, " "),
		Mode:    pane.Mode,
	}
	if spec.Mode == "" {
		spec.Mode = ViewportModeFit
	}
	if len(pane.Tags) > 0 {
		spec.Tag = formatTagExpression(pane.Tags)
	}
	if pane.TerminalID != "" {
		spec.HintID = pane.TerminalID
	}
	return spec
}

func exportFloatingEntrySpecs(tab *Tab, panes map[string]*Pane) []FloatingEntrySpec {
	if tab == nil || len(tab.Floating) == 0 {
		return nil
	}
	out := make([]FloatingEntrySpec, 0, len(tab.Floating))
	for _, entry := range tab.Floating {
		if entry == nil {
			continue
		}
		pane := panes[entry.PaneID]
		if pane == nil {
			continue
		}
		out = append(out, FloatingEntrySpec{
			Terminal: exportTerminalSpec(pane),
			Width:    entry.Rect.W,
			Height:   entry.Rect.H,
		})
	}
	return out
}

func formatTagExpression(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+tags[key])
	}
	return strings.Join(parts, ",")
}

func parseTagExpressionMap(expr string) map[string]string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			out[part] = ""
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func commandStringToSlice(command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	return strings.Fields(command)
}

func firstCommandWord(command []string) string {
	if len(command) == 0 {
		return ""
	}
	return command[0]
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func defaultTerminalState(state string) string {
	if strings.TrimSpace(state) == "" {
		return "running"
	}
	return state
}

func defaultViewportMode(mode, fallback ViewportMode) ViewportMode {
	if mode == "" {
		return fallback
	}
	return mode
}
