package app

import (
	"strings"

	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/shared"
)

func chromeConfigFromShared(cfg shared.ChromeConfig) render.UIChromeConfig {
	ui := render.DefaultUIChromeConfig()
	if cfg.PaneTop != nil {
		if slots := mapChromeSlots(cfg.PaneTop); slots != nil {
			ui.PaneChrome.Top = slots
		}
	}
	if cfg.StatusLeft != nil {
		if slots := mapChromeSlots(cfg.StatusLeft); slots != nil {
			ui.StatusBar.Left = slots
		}
	}
	if cfg.StatusRight != nil {
		if slots := mapChromeSlots(cfg.StatusRight); slots != nil {
			ui.StatusBar.Right = slots
		}
	}
	if cfg.TabLeft != nil {
		if slots := mapChromeSlots(cfg.TabLeft); slots != nil {
			ui.TabBar.Left = slots
		}
	}
	return ui
}

func themeConfigFromShared(cfg shared.ThemeConfig) render.UIThemeConfig {
	return render.UIThemeConfig{
		Accent:        cfg.Accent,
		Success:       cfg.Success,
		Warning:       cfg.Warning,
		Danger:        cfg.Danger,
		Info:          cfg.Info,
		PanelBorder:   cfg.PanelBorder,
		PanelBorder2:  cfg.PanelBorder2,
		TabActiveBG:   cfg.TabActiveBG,
		TabActiveFG:   cfg.TabActiveFG,
		TabInactiveBG: cfg.TabInactiveBG,
		TabInactiveFG: cfg.TabInactiveFG,
		TabCreateBG:   cfg.TabCreateBG,
		TabCreateFG:   cfg.TabCreateFG,
	}
}

func mapChromeSlots(values []string) []render.ChromeSlotID {
	if values == nil {
		return nil
	}
	out := make([]render.ChromeSlotID, 0, len(values))
	seen := make(map[render.ChromeSlotID]struct{}, len(values))
	for _, value := range values {
		slot, ok := parseChromeSlot(value)
		if !ok {
			continue
		}
		if _, exists := seen[slot]; exists {
			continue
		}
		seen[slot] = struct{}{}
		out = append(out, slot)
	}
	if len(values) > 0 && len(out) == 0 {
		return nil
	}
	return out
}

func parseChromeSlot(value string) (render.ChromeSlotID, bool) {
	switch strings.TrimSpace(value) {
	case string(render.SlotPaneTitle):
		return render.SlotPaneTitle, true
	case string(render.SlotPaneState):
		return render.SlotPaneState, true
	case string(render.SlotPaneShare):
		return render.SlotPaneShare, true
	case string(render.SlotPaneRole):
		return render.SlotPaneRole, true
	case string(render.SlotPaneCopyTime):
		return render.SlotPaneCopyTime, true
	case string(render.SlotPaneCopyRow):
		return render.SlotPaneCopyRow, true
	case string(render.SlotPaneActions):
		return render.SlotPaneActions, true
	case string(render.SlotStatusMode):
		return render.SlotStatusMode, true
	case string(render.SlotStatusHints):
		return render.SlotStatusHints, true
	case string(render.SlotStatusTokens):
		return render.SlotStatusTokens, true
	case string(render.SlotTabWorkspace):
		return render.SlotTabWorkspace, true
	case string(render.SlotTabTabs):
		return render.SlotTabTabs, true
	case string(render.SlotTabCreate):
		return render.SlotTabCreate, true
	case string(render.SlotTabActions):
		return render.SlotTabActions, true
	default:
		return "", false
	}
}

func (m *Model) chromeConfig() render.UIChromeConfig {
	if m == nil {
		return render.DefaultUIChromeConfig()
	}
	return m.chrome
}

func (m *Model) SetChromeConfig(cfg render.UIChromeConfig) {
	if m == nil {
		return
	}
	m.chrome = cfg
	if m.render != nil {
		m.render.Invalidate()
	}
}
