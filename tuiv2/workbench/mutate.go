package workbench

import (
	"fmt"
	"math"
	"strings"
)

const (
	minFloatingWidth  = 10
	minFloatingHeight = 4
)

// findTab searches all workspaces for the tab with the given ID and returns it
// together with its parent workspace. Returns an error if not found.
func (w *Workbench) findTab(tabID string) (*WorkspaceState, *TabState, error) {
	for _, ws := range w.store {
		for _, tab := range ws.Tabs {
			if tab != nil && tab.ID == tabID {
				return ws, tab, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("workbench: tab %q not found", tabID)
}

// CreateTab adds a new, empty tab to the workspace identified by wsName.
// Returns an error if the workspace does not exist or the tab ID is already
// taken within that workspace.
func (w *Workbench) CreateTab(wsName, tabID, tabName string) error {
	ws, ok := w.store[wsName]
	if !ok {
		return fmt.Errorf("workbench: workspace %q not found", wsName)
	}
	for _, t := range ws.Tabs {
		if t != nil && t.ID == tabID {
			return fmt.Errorf("workbench: tab ID %q already exists in workspace %q", tabID, wsName)
		}
	}
	ws.appendTab(&TabState{
		ID:    tabID,
		Name:  tabName,
		Panes: make(map[string]*PaneState),
	})
	w.touch()
	return nil
}

// CreateFirstPane creates the first pane in a tab. It sets up the root
// LayoutNode as a single leaf and updates ActivePaneID. Returns an error if
// the tab already has a root (use SplitPane for subsequent panes) or if the
// tab does not exist.
func (w *Workbench) CreateFirstPane(tabID, paneID string) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if tab.Root != nil {
		return fmt.Errorf("workbench: tab %q already has a root layout node; use SplitPane", tabID)
	}
	if tab.Panes == nil {
		tab.Panes = make(map[string]*PaneState)
	}
	tab.Panes[paneID] = &PaneState{ID: paneID}
	tab.Root = NewLeaf(paneID)
	tab.ActivePaneID = paneID
	w.touch()
	return nil
}

// SplitPane replaces the leaf node for paneID with an internal split node
// whose First child is the existing pane and Second child is a new leaf for
// newPaneID. The split uses a default ratio of 0.5. ActivePaneID is updated
// to newPaneID on success.
//
// Returns an error if:
//   - the tab does not exist
//   - paneID is not present in the tab
//   - newPaneID already exists in the tab
//   - the layout tree contains no leaf for paneID (inconsistent state)
func (w *Workbench) SplitPane(tabID, paneID, newPaneID string, dir SplitDirection) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if !tab.hasPane(paneID) {
		return fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	if _, dup := tab.Panes[newPaneID]; dup {
		return fmt.Errorf("workbench: pane ID %q already exists in tab %q", newPaneID, tabID)
	}

	// Replace the leaf for paneID in the layout tree.
	newRoot, replaced := replaceLeaf(tab.Root, paneID, newPaneID, dir)
	if !replaced {
		return fmt.Errorf("workbench: no layout leaf found for pane %q in tab %q", paneID, tabID)
	}
	tab.Root = newRoot

	tab.Panes[newPaneID] = &PaneState{ID: newPaneID}
	tab.ActivePaneID = newPaneID
	w.touch()
	return nil
}

// replaceLeaf recursively replaces the leaf node matching paneID with a new
// internal split node. Returns the (possibly new) root and whether the
// replacement was made.
func replaceLeaf(n *LayoutNode, paneID, newPaneID string, dir SplitDirection) (*LayoutNode, bool) {
	if n == nil {
		return n, false
	}
	if n.IsLeaf() {
		if n.PaneID != paneID {
			return n, false
		}
		// Wrap the existing leaf with a new split node.
		return &LayoutNode{
			Direction: dir,
			Ratio:     0.5,
			First:     NewLeaf(paneID),
			Second:    NewLeaf(newPaneID),
		}, true
	}
	// Recurse into children.
	newFirst, done := replaceLeaf(n.First, paneID, newPaneID, dir)
	if done {
		n.First = newFirst
		return n, true
	}
	newSecond, done := replaceLeaf(n.Second, paneID, newPaneID, dir)
	if done {
		n.Second = newSecond
		return n, true
	}
	return n, false
}

// FocusPane sets tab.ActivePaneID to paneID. Returns an error if the tab or
// pane does not exist.
func (w *Workbench) FocusPane(tabID, paneID string) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if !tab.hasPane(paneID) {
		return fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	tab.ActivePaneID = paneID
	w.touch()
	return nil
}

// ClosePane removes a pane from the tab layout and returns the detached
// terminal binding, if any.
func (w *Workbench) ClosePane(tabID, paneID string) (string, error) {
	ws, tab, err := w.findTab(tabID)
	if err != nil {
		return "", err
	}
	terminalID, removed := tab.removePane(paneID)
	if !removed {
		return "", fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	if len(tab.Panes) == 0 {
		ws.closeTabByID(tabID)
	}
	w.touch()
	return terminalID, nil
}

// SwitchTab activates the workspace tab at index.
func (w *Workbench) SwitchTab(wsName string, index int) error {
	ws, ok := w.store[wsName]
	if !ok {
		return fmt.Errorf("workbench: workspace %q not found", wsName)
	}
	if !ws.activateTab(index) {
		return fmt.Errorf("workbench: tab index %d out of range in workspace %q", index, wsName)
	}
	w.touch()
	return nil
}

// CloseTab removes the identified tab and updates the workspace active index.
func (w *Workbench) CloseTab(tabID string) error {
	ws, _, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if !ws.closeTabByID(tabID) {
		return fmt.Errorf("workbench: tab %q not found", tabID)
	}
	w.touch()
	return nil
}

// RenameTab updates the tab name in-place.
func (w *Workbench) RenameTab(tabID, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("workbench: tab name must not be empty")
	}
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	tab.Name = name
	w.touch()
	return nil
}

// CreateWorkspace adds a new empty workspace. Returns an error if name is
// empty or already taken.
func (w *Workbench) CreateWorkspace(name string) error {
	if name == "" {
		return fmt.Errorf("workbench: workspace name must not be empty")
	}
	if _, exists := w.store[name]; exists {
		return fmt.Errorf("workbench: workspace %q already exists", name)
	}
	w.AddWorkspace(name, &WorkspaceState{Name: name, ActiveTab: -1})
	return nil
}

// DeleteWorkspace removes the workspace identified by name. Returns an error
// if the workspace does not exist.
func (w *Workbench) DeleteWorkspace(name string) error {
	if _, exists := w.store[name]; !exists {
		return fmt.Errorf("workbench: workspace %q not found", name)
	}
	w.RemoveWorkspace(name)
	return nil
}

// RenameWorkspace renames an existing workspace while preserving order and
// current selection.
func (w *Workbench) RenameWorkspace(oldName, newName string) error {
	if w == nil {
		return fmt.Errorf("workbench: nil workbench")
	}
	if oldName == "" || newName == "" {
		return fmt.Errorf("workbench: workspace name must not be empty")
	}
	if oldName == newName {
		return nil
	}
	ws, exists := w.store[oldName]
	if !exists {
		return fmt.Errorf("workbench: workspace %q not found", oldName)
	}
	if _, exists := w.store[newName]; exists {
		return fmt.Errorf("workbench: workspace %q already exists", newName)
	}
	delete(w.store, oldName)
	ws.Name = newName
	w.store[newName] = ws
	for index, name := range w.order {
		if name == oldName {
			w.order[index] = newName
			break
		}
	}
	if w.current == oldName {
		w.current = newName
	}
	w.touch()
	return nil
}

// SwitchWorkspaceByOffset activates the workspace relative to the current
// workspace, wrapping around the workspace order.
func (w *Workbench) SwitchWorkspaceByOffset(delta int) error {
	if w == nil || len(w.order) == 0 {
		return fmt.Errorf("workbench: no workspaces available")
	}
	currentIndex := -1
	for index, name := range w.order {
		if name == w.current {
			currentIndex = index
			break
		}
	}
	if currentIndex < 0 {
		currentIndex = 0
	}
	nextIndex := (currentIndex + delta + len(w.order)) % len(w.order)
	w.current = w.order[nextIndex]
	w.touch()
	return nil
}

// CreateFloatingPane adds a new floating pane to the tab identified by tabID.
func (w *Workbench) CreateFloatingPane(tabID, paneID string, rect Rect) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if _, exists := tab.Panes[paneID]; exists {
		return fmt.Errorf("workbench: pane %q already exists in tab %q", paneID, tabID)
	}
	if tab.Panes == nil {
		tab.Panes = make(map[string]*PaneState)
	}
	tab.Panes[paneID] = &PaneState{ID: paneID}
	z := 0
	for _, floating := range tab.Floating {
		if floating != nil && floating.Z >= z {
			z = floating.Z + 1
		}
	}
	tab.Floating = append(tab.Floating, &FloatingState{PaneID: paneID, Rect: rect, Z: z})
	tab.FloatingVisible = true
	w.touch()
	return nil
}

// AdjustPaneRatio walks the layout tree of the tab and adjusts the split ratio
// of the first ancestor node whose split axis aligns with dir. delta is added
// to (or subtracted from) Ratio and clamped to [0.1, 0.9].
func (w *Workbench) AdjustPaneRatio(tabID, paneID string, dir Direction, delta float64) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	if tab.Root == nil {
		return nil
	}
	adjustRatioForPane(tab.Root, paneID, dir, delta)
	w.touch()
	return nil
}

func containsPane(node *LayoutNode, paneID string) bool {
	if node == nil {
		return false
	}
	if node.IsLeaf() {
		return node.PaneID == paneID
	}
	return containsPane(node.First, paneID) || containsPane(node.Second, paneID)
}

func containsNode(node, target *LayoutNode) bool {
	if node == nil || target == nil {
		return false
	}
	if node == target {
		return true
	}
	return containsNode(node.First, target) || containsNode(node.Second, target)
}

// adjustRatioForPane finds the nearest ancestor split node whose axis aligns
// with dir and adjusts its ratio so that the pane containing paneID grows or
// shrinks in the requested direction.
func adjustRatioForPane(node *LayoutNode, paneID string, dir Direction, delta float64) bool {
	if node == nil || node.IsLeaf() {
		return false
	}
	horizontal := node.Direction == SplitHorizontal && (dir == DirectionUp || dir == DirectionDown)
	vertical := node.Direction == SplitVertical && (dir == DirectionLeft || dir == DirectionRight)
	if horizontal || vertical {
		inFirst := containsPane(node.First, paneID)
		inSecond := containsPane(node.Second, paneID)
		if inFirst || inSecond {
			grow := dir == DirectionRight || dir == DirectionDown
			if inFirst {
				if grow {
					node.Ratio += delta
				} else {
					node.Ratio -= delta
				}
			} else {
				if grow {
					node.Ratio -= delta
				} else {
					node.Ratio += delta
				}
			}
			if node.Ratio < 0.1 {
				node.Ratio = 0.1
			}
			if node.Ratio > 0.9 {
				node.Ratio = 0.9
			}
			return true
		}
	}
	if adjustRatioForPane(node.First, paneID, dir, delta) {
		return true
	}
	return adjustRatioForPane(node.Second, paneID, dir, delta)
}

// BalancePanes resets all pane split ratios to equal in the given tab.
func (w *Workbench) BalancePanes(tabID string) {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return
	}
	resetNodeRatios(tab.Root)
	w.touch()
}

func resetNodeRatios(node *LayoutNode) {
	if node == nil || node.IsLeaf() {
		return
	}
	node.Ratio = 0.5
	resetNodeRatios(node.First)
	resetNodeRatios(node.Second)
}

// CycleLayout cycles through layout presets for the given tab.
func (w *Workbench) CycleLayout(tabID string) {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return
	}
	tab.LayoutPreset = (tab.LayoutPreset + 1) % 3
	w.touch()
}

// BindPaneTerminal sets the TerminalID for a pane. This is the sole write
// path for associating a backend terminal with a pane. Returns an error if
// the tab or pane does not exist.
func (w *Workbench) BindPaneTerminal(tabID, paneID, terminalID string) error {
	_, tab, err := w.findTab(tabID)
	if err != nil {
		return err
	}
	pane := tab.Panes[paneID]
	if pane == nil {
		return fmt.Errorf("workbench: pane %q not found in tab %q", paneID, tabID)
	}
	pane.TerminalID = terminalID
	w.touch()
	return nil
}

// MoveFloatingPane moves a floating pane to a new position.
func (w *Workbench) MoveFloatingPane(tabID, paneID string, x, y int) bool {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return false
	}

	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			floating.Rect.X = maxInt(0, x)
			floating.Rect.Y = maxInt(0, y)
			w.touch()
			return true
		}
	}
	return false
}

func (w *Workbench) MoveFloatingPaneBy(tabID, paneID string, dx, dy int) bool {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return false
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			return w.MoveFloatingPane(tabID, paneID, floating.Rect.X+dx, floating.Rect.Y+dy)
		}
	}
	return false
}

// ResizeFloatingPane resizes a floating pane.
func (w *Workbench) ResizeFloatingPane(tabID, paneID string, width, height int) bool {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return false
	}

	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			floating.Rect.W = maxInt(minFloatingWidth, width)
			floating.Rect.H = maxInt(minFloatingHeight, height)
			w.touch()
			return true
		}
	}
	return false
}

func (w *Workbench) ResizeFloatingPaneBy(tabID, paneID string, dw, dh int) bool {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return false
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			return w.ResizeFloatingPane(tabID, paneID, floating.Rect.W+dw, floating.Rect.H+dh)
		}
	}
	return false
}

// ResizeSplit repositions the divider for a concrete split node inside the
// tab's layout tree. splitRoot must be the rect occupied by target.
func (w *Workbench) ResizeSplit(tabID string, target *LayoutNode, splitRoot Rect, x, y, offsetX, offsetY int) bool {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil || tab.Root == nil || target == nil {
		return false
	}
	if !containsNode(tab.Root, target) {
		return false
	}
	if !target.SetRatioFromDivider(splitRoot, x, y, offsetX, offsetY) {
		return false
	}
	w.touch()
	return true
}

// ReorderFloatingPane moves a floating pane to the top of the Z-order.
func (w *Workbench) ReorderFloatingPane(tabID, paneID string, toTop bool) bool {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return false
	}

	if !toTop {
		return false
	}

	// 找到目标浮动窗口的索引
	targetIndex := -1
	for i, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 || targetIndex == len(tab.Floating)-1 {
		return false
	}

	// 移动到最后（Z-order 最高）
	target := tab.Floating[targetIndex]
	tab.Floating = append(tab.Floating[:targetIndex], tab.Floating[targetIndex+1:]...)
	tab.Floating = append(tab.Floating, target)
	normalizeFloatingZ(tab.Floating)
	w.touch()
	return true
}

func (w *Workbench) CenterFloatingPane(tabID, paneID string, bounds Rect) bool {
	_, tab, err := w.findTab(tabID)
	if err != nil || tab == nil {
		return false
	}
	for _, floating := range tab.Floating {
		if floating == nil || floating.PaneID != paneID {
			continue
		}
		width := floating.Rect.W
		height := floating.Rect.H
		if width <= 0 || height <= 0 {
			return false
		}
		targetX := maxInt(0, (bounds.W-width)/2)
		targetY := maxInt(0, (bounds.H-height)/2)
		floating.Rect.X = targetX
		floating.Rect.Y = targetY
		w.touch()
		return true
	}
	return false
}

func (w *Workbench) ReflowFloatingPanes(from, to Rect) bool {
	if w == nil || from.W <= 0 || from.H <= 0 || to.W <= 0 || to.H <= 0 {
		return false
	}
	if from.W == to.W && from.H == to.H {
		return false
	}

	changed := false
	for _, ws := range w.store {
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			for _, floating := range tab.Floating {
				if floating == nil {
					continue
				}
				next := reflowFloatingRect(floating.Rect, from, to)
				if next != floating.Rect {
					floating.Rect = next
					changed = true
				}
			}
		}
	}
	if changed {
		w.touch()
	}
	return changed
}

func (w *Workbench) ClampFloatingPanesToBounds(bounds Rect) bool {
	if w == nil || bounds.W <= 0 || bounds.H <= 0 {
		return false
	}

	changed := false
	for _, ws := range w.store {
		if ws == nil {
			continue
		}
		for _, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			for _, floating := range tab.Floating {
				if floating == nil {
					continue
				}
				next := clampFloatingRectToBounds(floating.Rect, bounds)
				if next != floating.Rect {
					floating.Rect = next
					changed = true
				}
			}
		}
	}
	if changed {
		w.touch()
	}
	return changed
}

func reflowFloatingRect(rect, from, to Rect) Rect {
	left := scaleAxis(rect.X, from.W, to.W)
	top := scaleAxis(rect.Y, from.H, to.H)
	right := scaleAxis(rect.X+rect.W, from.W, to.W)
	bottom := scaleAxis(rect.Y+rect.H, from.H, to.H)

	width := right - left
	height := bottom - top
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}

	width = clampFloatingDimension(width, to.W, minFloatingWidth)
	height = clampFloatingDimension(height, to.H, minFloatingHeight)
	left = clampFloatingOffset(left, width, to.W)
	top = clampFloatingOffset(top, height, to.H)

	return Rect{X: left, Y: top, W: width, H: height}
}

func clampFloatingRectToBounds(rect, bounds Rect) Rect {
	width := clampFloatingDimension(rect.W, bounds.W, minFloatingWidth)
	height := clampFloatingDimension(rect.H, bounds.H, minFloatingHeight)
	left := clampFloatingOffset(rect.X, width, bounds.W)
	top := clampFloatingOffset(rect.Y, height, bounds.H)
	return Rect{X: left, Y: top, W: width, H: height}
}

func scaleAxis(value, from, to int) int {
	if from <= 0 || to <= 0 {
		return 0
	}
	return int(math.Round(float64(value) * float64(to) / float64(from)))
}

func clampFloatingDimension(value, limit, minimum int) int {
	if limit <= 0 {
		return 1
	}
	maximum := maxFloatingDimension(limit)
	if value > maximum {
		value = maximum
	}
	if limit >= minimum && value < minimum {
		value = minimum
	}
	if value > maximum {
		value = maximum
	}
	if value < 1 {
		value = 1
	}
	return value
}

func maxFloatingDimension(limit int) int {
	if limit <= 1 {
		return 1
	}
	return limit - 1
}

func clampFloatingOffset(offset, size, limit int) int {
	if limit <= 0 {
		return 0
	}
	if size >= limit {
		return 0
	}
	if offset < 0 {
		return 0
	}
	maxOffset := limit - size
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

func normalizeFloatingZ(entries []*FloatingState) {
	for i, floating := range entries {
		if floating == nil {
			continue
		}
		floating.Z = i
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
