package render

import (
	"strconv"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/shortcut"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestFooterHitRegionCarriesExactShortcutInvocation(t *testing.T) {
	invocation, _, err := shortcut.ParseInvocation("panel.close")
	if err != nil {
		t.Fatal(err)
	}
	footer := FooterVM{Visible: true, Mode: "pane", ActionTokens: []FooterActionVM{{Key: "q/w", Label: "CLOSE", ActionID: ActionPaneFooterClose.String(), Invocation: invocation, Click: shortcut.ClickClickable}}}
	regions := appendFooterHitRegions(nil, footer, Rect{W: 80, H: 1}, Rect{}, Rect{W: 80, H: 1})
	if len(regions) != 1 || regions[0].Invocation.Signature() != invocation.Signature() {
		t.Fatalf("footer hit region lost invocation: %#v", regions)
	}
}

func TestAggregatedDifferentInvocationsAreHintOnly(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell().SetInteractionMode(state.InteractionModeTab)}
	actions := footerActionCatalogFromShortcuts("tab", root)
	for _, action := range actions {
		if action.ActionID == ActionTabSwitch.String() && (action.Click == shortcut.ClickClickable || action.Invocation.ID != "") {
			t.Fatalf("tab jump aggregation must be hint-only: %#v", action)
		}
	}
	footer := FooterVM{Visible: true, Mode: "tab", ActionTokens: actions}
	regions := appendFooterHitRegions(nil, footer, Rect{W: 200, H: 1}, Rect{}, Rect{W: 200, H: 1})
	for _, region := range regions {
		if region.ActionID == ActionTabSwitch.String() {
			t.Fatalf("hint-only tab jump must not create hit region: %#v", region)
		}
	}
}

func TestFooterZeroClickPolicyDoesNotCreateHitRegion(t *testing.T) {
	footer := FooterVM{Visible: true, ActionTokens: []FooterActionVM{{
		Key:        "x",
		Label:      "ACTION",
		ActionID:   "legacy.action",
		Invocation: shortcut.ActionInvocation{ID: "panel.close"},
	}}}
	regions := appendFooterHitRegions(nil, footer, Rect{W: 80, H: 1}, Rect{}, Rect{W: 80, H: 1})
	if len(regions) != 0 {
		t.Fatalf("zero click policy must be non-clickable: %#v", regions)
	}
}

func TestOverlayShortcutBindingPreservesTargetContextAndHonorsCatalog(t *testing.T) {
	regions := []HitRegion{{
		Kind:     HitRegionContentAction,
		Rect:     Rect{W: 20, H: 1},
		PaneID:   "pane-2",
		Row:      3,
		ActionID: ActionWorkbenchOpen.String(),
	}}
	bound := bindOverlayShortcutInvocations(OverlayWorkbenchTree, regions, state.TUIShortcutConfig{})
	if len(bound) != 1 || bound[0].Invocation.ID != "workbench_tree.open" || bound[0].PaneID != "pane-2" || bound[0].Row != 3 {
		t.Fatalf("overlay shortcut lost invocation or target context: %#v", bound)
	}

	emptyCatalog := state.TUIShortcutConfig{Configured: true, Scenes: map[string]state.TUIShortcutSceneConfig{
		"workbench_tree": {Bindings: map[string]state.TUIShortcutBindingConfig{}},
	}}
	if got := bindOverlayShortcutInvocations(OverlayWorkbenchTree, regions, emptyCatalog); len(got) != 0 {
		t.Fatalf("unconfigured overlay shortcut must not retain legacy click fallback: %#v", got)
	}
}

func TestFloatingOverviewRowsUseOpenInvocationBeyondNumericSummonRange(t *testing.T) {
	regions := make([]HitRegion, 10)
	for index := range regions {
		regions[index] = HitRegion{
			Kind:     HitRegionContentAction,
			Rect:     Rect{Y: index, W: 20, H: 1},
			PaneID:   "floating-" + strconv.Itoa(index+1),
			Row:      index,
			ActionID: ActionFloatingSummon.String(),
		}
	}
	shortcuts := state.TUIShortcutConfig{Configured: true, Scenes: map[string]state.TUIShortcutSceneConfig{
		"floating_overview": {Bindings: map[string]state.TUIShortcutBindingConfig{
			"enter": {Action: "floating_overview.open"},
		}},
	}}
	bound := bindOverlayShortcutInvocations(OverlayFloatingOverview, regions, shortcuts)
	if len(bound) != 10 {
		t.Fatalf("all overview rows must remain clickable without numeric summon bindings: %d", len(bound))
	}
	if bound[9].Invocation.ID != "floating_overview.open" || bound[9].Row != 9 || bound[9].PaneID != "floating-10" {
		t.Fatalf("tenth overview row lost open invocation or context: %#v", bound[9])
	}
}
