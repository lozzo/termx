package app

import (
	"reflect"
	"testing"

	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/shared"
)

func TestChromeConfigFromSharedUsesDefaultsForEmptyConfig(t *testing.T) {
	got := chromeConfigFromShared(shared.ChromeConfig{})
	want := render.DefaultUIChromeConfig()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected default chrome config:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestChromeConfigFromSharedMapsExternalSlots(t *testing.T) {
	got := chromeConfigFromShared(shared.ChromeConfig{
		PaneTop:     []string{"pane.title", "pane.actions"},
		StatusLeft:  []string{"status.hints"},
		StatusRight: []string{},
		TabLeft:     []string{"tab.workspace", "tab.tabs"},
	})
	if !reflect.DeepEqual(got.PaneChrome.Top, []render.ChromeSlotID{render.SlotPaneTitle, render.SlotPaneActions}) {
		t.Fatalf("unexpected pane slots: %#v", got.PaneChrome.Top)
	}
	if !reflect.DeepEqual(got.StatusBar.Left, []render.ChromeSlotID{render.SlotStatusHints}) {
		t.Fatalf("unexpected status left slots: %#v", got.StatusBar.Left)
	}
	if len(got.StatusBar.Right) != 0 {
		t.Fatalf("expected empty status right slots, got %#v", got.StatusBar.Right)
	}
	if !reflect.DeepEqual(got.TabBar.Left, []render.ChromeSlotID{render.SlotTabWorkspace, render.SlotTabTabs}) {
		t.Fatalf("unexpected tab slots: %#v", got.TabBar.Left)
	}
}

func TestThemeConfigFromSharedMapsOverrides(t *testing.T) {
	got := themeConfigFromShared(shared.ThemeConfig{
		Accent:      "#8b5cf6",
		PanelBorder: "#4b5563",
		TabActiveBG: "#111827",
	})
	want := render.UIThemeConfig{
		Accent:      "#8b5cf6",
		PanelBorder: "#4b5563",
		TabActiveBG: "#111827",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected theme config:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestModelRenderVMUsesChromeConfig(t *testing.T) {
	m := New(shared.Config{}, nil, nil)
	cfg := render.UIChromeConfig{
		TabBar: render.TabBarConfig{Left: []render.ChromeSlotID{render.SlotTabWorkspace, render.SlotTabTabs}},
		StatusBar: render.StatusBarConfig{Left: []render.ChromeSlotID{render.SlotStatusHints}, Right: []render.ChromeSlotID{}},
		PaneChrome: render.PaneChromeConfig{Top: []render.ChromeSlotID{render.SlotPaneTitle, render.SlotPaneActions}},
	}
	m.SetChromeConfig(cfg)
	vm := m.renderVM()
	if reflect.DeepEqual(vm.Chrome, render.DefaultUIChromeConfig()) {
		t.Fatalf("expected custom chrome config to reach render VM")
	}
	if len(vm.Chrome.TabBar.Left) != 2 || vm.Chrome.TabBar.Left[0] != render.SlotTabWorkspace || vm.Chrome.TabBar.Left[1] != render.SlotTabTabs {
		t.Fatalf("unexpected tab bar config in render VM: %#v", vm.Chrome)
	}
	if len(vm.Chrome.StatusBar.Right) != 0 {
		t.Fatalf("expected empty status right slots, got %#v", vm.Chrome.StatusBar.Right)
	}
}

func TestModelInitialConfigsUseSharedConfig(t *testing.T) {
	m := New(shared.Config{
		Chrome: shared.ChromeConfig{TabLeft: []string{"tab.workspace", "tab.tabs"}, StatusRight: []string{}},
		Theme:  shared.ThemeConfig{Accent: "#8b5cf6", TabActiveBG: "#111827"},
	}, nil, nil)
	vm := m.renderVM()
	if !reflect.DeepEqual(vm.Chrome.TabBar.Left, []render.ChromeSlotID{render.SlotTabWorkspace, render.SlotTabTabs}) {
		t.Fatalf("unexpected initial tab config: %#v", vm.Chrome.TabBar.Left)
	}
	if len(vm.Chrome.StatusBar.Right) != 0 {
		t.Fatalf("expected initial empty status right slots, got %#v", vm.Chrome.StatusBar.Right)
	}
	if vm.Theme.Accent != "#8b5cf6" || vm.Theme.TabActiveBG != "#111827" {
		t.Fatalf("unexpected initial theme config: %#v", vm.Theme)
	}
}
