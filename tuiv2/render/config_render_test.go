package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestUIThemeForVMAppliesThemeOverrides(t *testing.T) {
	vm := RenderVM{
		Theme: UIThemeConfig{
			Accent:      "#123456",
			PanelBorder: "#654321",
			TabActiveBG: "#111111",
		},
	}
	theme := uiThemeForVM(vm)
	if theme.chromeAccent != "#123456" {
		t.Fatalf("chrome accent = %q, want %q", theme.chromeAccent, "#123456")
	}
	if theme.panelBorder != "#654321" {
		t.Fatalf("panel border = %q, want %q", theme.panelBorder, "#654321")
	}
	if theme.tabActiveBG != "#111111" {
		t.Fatalf("tab active bg = %q, want %q", theme.tabActiveBG, "#111111")
	}
}

func TestNormalizeUIChromeConfigPreservesExplicitEmptySlots(t *testing.T) {
	cfg := normalizeUIChromeConfig(UIChromeConfig{
		PaneChrome: PaneChromeConfig{Top: []ChromeSlotID{}},
		StatusBar: StatusBarConfig{
			Left:  []ChromeSlotID{},
			Right: []ChromeSlotID{},
		},
		TabBar: TabBarConfig{Left: []ChromeSlotID{}},
	})
	if cfg.PaneChrome.Top == nil || len(cfg.PaneChrome.Top) != 0 {
		t.Fatalf("expected explicit empty pane-top config, got %#v", cfg.PaneChrome.Top)
	}
	if cfg.StatusBar.Left == nil || len(cfg.StatusBar.Left) != 0 {
		t.Fatalf("expected explicit empty status-left config, got %#v", cfg.StatusBar.Left)
	}
	if cfg.StatusBar.Right == nil || len(cfg.StatusBar.Right) != 0 {
		t.Fatalf("expected explicit empty status-right config, got %#v", cfg.StatusBar.Right)
	}
	if cfg.TabBar.Left == nil || len(cfg.TabBar.Left) != 0 {
		t.Fatalf("expected explicit empty tab-left config, got %#v", cfg.TabBar.Left)
	}
}

func TestRenderStatusBarHonorsExplicitEmptyRightSlotConfig(t *testing.T) {
	state := VisibleRenderState{
		TermSize: TermSize{Width: 80, Height: 20},
		Chrome: UIChromeConfig{StatusBar: StatusBarConfig{
			Left:  []ChromeSlotID{SlotStatusHints},
			Right: []ChromeSlotID{},
		}},
		InputMode:   string(input.ModePane),
		StatusHints: []string{"r RECONNECT"},
		Runtime: &VisibleRuntimeStateProxy{
			Terminals: []runtime.VisibleTerminal{{TerminalID: "term-1"}},
		},
	}

	line := xansi.Strip(renderStatusBar(state))
	if !strings.Contains(line, "[r] RECONNECT") {
		t.Fatalf("expected left hints to remain visible, got %q", line)
	}
	if strings.Contains(line, "terminals:1") {
		t.Fatalf("expected right tokens hidden by explicit empty slot config, got %q", line)
	}
}

func TestDrawPaneFrameHonorsPaneTopSlotConfig(t *testing.T) {
	canvas := newComposedCanvas(30, 6)
	drawPaneFrame(
		canvas,
		workbench.Rect{X: 0, Y: 0, W: 30, H: 6},
		false,
		false,
		"demo",
		paneBorderInfo{},
		defaultUITheme(),
		paneOverflowHints{},
		true,
		false,
		UIChromeConfig{PaneChrome: PaneChromeConfig{Top: []ChromeSlotID{SlotPaneActions}}},
	)

	line := xansi.Strip(canvas.cachedContentLines()[0])
	if strings.Contains(line, "demo") {
		t.Fatalf("expected pane title hidden by slot config, got %q", line)
	}
}

func TestRenderAltScreenTopBorderLineHonorsPaneTopSlotConfig(t *testing.T) {
	line := xansi.Strip(renderAltScreenTopBorderLine(paneRenderEntry{
		Rect:   workbench.Rect{X: 0, Y: 0, W: 30, H: 6},
		Title:  "demo",
		Theme:  defaultUITheme(),
		Active: true,
		Chrome: UIChromeConfig{PaneChrome: PaneChromeConfig{Top: []ChromeSlotID{SlotPaneActions}}},
	}))
	if strings.Contains(line, "demo") {
		t.Fatalf("expected alt-screen title hidden by slot config, got %q", line)
	}
}

func TestCachedFrameMissesWhenThemeConfigChanges(t *testing.T) {
	vm := makeTestVM()
	current := vm
	coordinator := NewCoordinatorWithVM(func() RenderVM { return current })
	_ = coordinator.RenderFrame()
	if _, _, ok := coordinator.CachedFrameAndCursor(); !ok {
		t.Fatal("expected cached frame after initial render")
	}
	current = WithRenderThemeConfig(current, UIThemeConfig{Accent: "#ff00aa"})
	if _, _, ok := coordinator.CachedFrameAndCursor(); ok {
		t.Fatal("expected cache miss after theme config change")
	}
}
