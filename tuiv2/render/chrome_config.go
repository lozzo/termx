package render

import "strings"

type ChromeSlotID string

const (
	SlotPaneTitle    ChromeSlotID = "pane.title"
	SlotPaneState    ChromeSlotID = "pane.state"
	SlotPaneShare    ChromeSlotID = "pane.share"
	SlotPaneRole     ChromeSlotID = "pane.role"
	SlotPaneSize     ChromeSlotID = "pane.size"
	SlotPaneCopyTime ChromeSlotID = "pane.copy_time"
	SlotPaneCopyRow  ChromeSlotID = "pane.copy_row"
	SlotPaneActions  ChromeSlotID = "pane.actions"

	SlotStatusMode   ChromeSlotID = "status.mode"
	SlotStatusHints  ChromeSlotID = "status.hints"
	SlotStatusTokens ChromeSlotID = "status.tokens"

	SlotTabWorkspace ChromeSlotID = "tab.workspace"
	SlotTabTabs      ChromeSlotID = "tab.tabs"
	SlotTabCreate    ChromeSlotID = "tab.create"
	SlotTabActions   ChromeSlotID = "tab.actions"
)

type UIChromeConfig struct {
	PaneChrome PaneChromeConfig
	StatusBar  StatusBarConfig
	TabBar     TabBarConfig
}

type PaneChromeConfig struct {
	Top []ChromeSlotID
}

type StatusBarConfig struct {
	Left  []ChromeSlotID
	Right []ChromeSlotID
}

type TabBarConfig struct {
	Left []ChromeSlotID
}

func DefaultUIChromeConfig() UIChromeConfig {
	return UIChromeConfig{
		PaneChrome: PaneChromeConfig{Top: []ChromeSlotID{
			SlotPaneTitle,
			SlotPaneState,
			SlotPaneShare,
			SlotPaneRole,
			SlotPaneCopyTime,
			SlotPaneCopyRow,
			SlotPaneActions,
		}},
		StatusBar: StatusBarConfig{
			Left:  []ChromeSlotID{SlotStatusMode, SlotStatusHints},
			Right: []ChromeSlotID{SlotStatusTokens},
		},
		TabBar: TabBarConfig{Left: []ChromeSlotID{SlotTabWorkspace, SlotTabTabs, SlotTabCreate, SlotTabActions}},
	}
}

func normalizeUIChromeConfig(cfg UIChromeConfig) UIChromeConfig {
	def := DefaultUIChromeConfig()
	cfg.PaneChrome.Top = normalizeChromeSlots(cfg.PaneChrome.Top, def.PaneChrome.Top)
	cfg.StatusBar.Left = normalizeChromeSlots(cfg.StatusBar.Left, def.StatusBar.Left)
	cfg.StatusBar.Right = normalizeChromeSlots(cfg.StatusBar.Right, def.StatusBar.Right)
	cfg.TabBar.Left = normalizeChromeSlots(cfg.TabBar.Left, def.TabBar.Left)
	return cfg
}

func normalizeChromeSlots(slots, defaults []ChromeSlotID) []ChromeSlotID {
	if slots == nil {
		return append([]ChromeSlotID(nil), defaults...)
	}
	out := make([]ChromeSlotID, 0, len(slots))
	seen := make(map[ChromeSlotID]struct{}, len(slots))
	for _, slot := range slots {
		if strings.TrimSpace(string(slot)) == "" {
			continue
		}
		if _, ok := seen[slot]; ok {
			continue
		}
		seen[slot] = struct{}{}
		out = append(out, slot)
	}
	return out
}

func (cfg UIChromeConfig) normalized() UIChromeConfig {
	return normalizeUIChromeConfig(cfg)
}

func (cfg UIChromeConfig) signature() string {
	cfg = cfg.normalized()
	sections := []string{
		joinChromeSlots(cfg.PaneChrome.Top),
		joinChromeSlots(cfg.StatusBar.Left),
		joinChromeSlots(cfg.StatusBar.Right),
		joinChromeSlots(cfg.TabBar.Left),
	}
	return strings.Join(sections, "\x1e")
}

func joinChromeSlots(slots []ChromeSlotID) string {
	if len(slots) == 0 {
		return ""
	}
	parts := make([]string, 0, len(slots))
	for _, slot := range slots {
		parts = append(parts, string(slot))
	}
	return strings.Join(parts, "\x1f")
}

func chromeSlotEnabled(slots []ChromeSlotID, target ChromeSlotID) bool {
	for _, slot := range slots {
		if slot == target {
			return true
		}
	}
	return false
}
