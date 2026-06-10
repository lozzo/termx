package render

import (
	"strings"
	"testing"
)

func TestChromePrimitivePaneActionSlotsMatchHitRegions(t *testing.T) {
	panel := PanelVM{
		ID:     "pane-1",
		Title:  "shell",
		Active: true,
		Chrome: PanelChromeVM{Actions: []ChromeActionVM{
			paneChromeActionVM(ActionPaneZoom, StyleAccent),
			paneChromeActionVM(ActionPaneSplitRight, StyleAccent),
			paneChromeActionVM(ActionPaneSplitDown, StyleAccent),
			paneChromeActionVM(ActionPaneClose, StyleAccent),
		}},
	}
	rect := Rect{W: 44, H: 8}
	primitive := PaneChromePrimitive(panel, rect, StyleAccent)
	regions := appendPaneActionRegions(nil, panel, rect, panel.ID, rect)

	if len(primitive.ActionSlots) != len(regions) {
		t.Fatalf("primitive action slots must match hit regions slots=%#v regions=%#v", primitive.ActionSlots, regions)
	}
	for index, slot := range primitive.ActionSlots {
		region := regions[index]
		if slot.ActionID != region.ActionID || slot.Rect != region.Rect || slot.Text == "" {
			t.Fatalf("slot %d must drive hit region slot=%#v region=%#v", index, slot, region)
		}
	}
}

func TestChromePrimitivePaneOwnerTokenDrivesTakeOwnerHitRegion(t *testing.T) {
	panel := PanelVM{
		ID:     "pane-2",
		Title:  "123",
		Active: true,
		Chrome: PanelChromeVM{
			Terminal: TerminalChromeVM{
				Title:       ChromeSlotVM{Text: "123", Style: StyleAccent},
				State:       ChromeSlotVM{Text: paneChromeRunningGlyph(), Style: StyleSuccess},
				AttachCount: 2,
				Owner:       ChromeSlotVM{Text: "◇ follow", Style: StyleMuted},
				TakeOwner:   true,
				TerminalID:  "term-1",
			},
			Actions: []ChromeActionVM{paneChromeActionVM(ActionPaneClose, StyleAccent)},
		},
	}
	rect := Rect{W: 72, H: 8}
	primitive := PaneChromePrimitive(panel, rect, StyleAccent)
	regions := appendPaneActionRegions(nil, panel, rect, panel.ID, rect)

	found := false
	for index, slot := range primitive.ActionSlots {
		if slot.ActionID != ActionTerminalTakeResizeOwner.String() {
			continue
		}
		found = true
		if !strings.Contains(slot.Text, "◇ follow") || regions[index].ActionID != slot.ActionID || regions[index].Rect != slot.Rect {
			t.Fatalf("owner token should be visible hit slot, slot=%#v regions=%#v", slot, regions)
		}
	}
	if !found {
		t.Fatalf("expected owner token action slot, got %#v", primitive.ActionSlots)
	}
}

func TestChromePrimitiveFloatingActionSlotsMatchHitRegions(t *testing.T) {
	rect := Rect{X: 10, Y: 4, W: 30, H: 8}
	primitive := FloatingChromePrimitive(FloatingVM{ID: "float-1", Rect: rect, Active: true}, rect, StyleAccent)
	regions := appendFloatingActionRegions(nil, FloatingVM{ID: "float-1", Rect: rect, Active: true}, rect, "float-1", Rect{W: 80, H: 24})

	if len(primitive.ActionSlots) != len(regions) {
		t.Fatalf("primitive floating slots must match hit regions slots=%#v regions=%#v", primitive.ActionSlots, regions)
	}
	for index, slot := range primitive.ActionSlots {
		region := regions[index]
		if slot.ActionID != region.ActionID || slot.Rect != region.Rect || slot.Text == "" {
			t.Fatalf("floating slot %d must drive hit region slot=%#v region=%#v", index, slot, region)
		}
	}
}

func TestChromePrimitiveFloatingTerminalOwnerTokenDrivesTakeOwnerHitRegion(t *testing.T) {
	rect := Rect{X: 2, Y: 1, W: 84, H: 8}
	floating := FloatingVM{ID: "float-1", Title: "123", Rect: rect, Active: true, Chrome: FloatingChromeVM{
		Terminal: TerminalChromeVM{
			Title:       ChromeSlotVM{Text: "123", Style: StyleAccent},
			State:       ChromeSlotVM{Text: paneChromeRunningGlyph(), Style: StyleSuccess},
			AttachCount: 2,
			Owner:       ChromeSlotVM{Text: "◇ follow", Style: StyleMuted},
			TakeOwner:   true,
			TerminalID:  "term-1",
		},
		Actions: []ChromeActionVM{
			paneChromeActionVM(ActionFloatingCenter, StyleAccent),
			paneChromeActionVM(ActionFloatingCollapse, StyleAccent),
			paneChromeActionVM(ActionPaneZoom, StyleAccent),
			paneChromeActionVM(ActionFloatingClose, StyleAccent),
		},
	}}
	primitive := FloatingChromePrimitive(floating, rect, StyleAccent)
	regions := appendFloatingActionRegions(nil, floating, rect, "float-1", Rect{W: 100, H: 24})

	found := false
	for _, slot := range primitive.ActionSlots {
		if slot.ActionID == ActionTerminalTakeResizeOwner.String() && strings.Contains(slot.Text, "◇ follow") {
			found = true
			if !hitRegionsContainActionRect(regions, slot.ActionID, slot.Rect) {
				t.Fatalf("floating owner token should drive matching hit region, slot=%#v regions=%#v", slot, regions)
			}
		}
	}
	if !found {
		t.Fatalf("expected floating owner token action slot, got %#v", primitive.ActionSlots)
	}
}

func hitRegionsContainActionRect(regions []HitRegion, actionID string, rect Rect) bool {
	for _, region := range regions {
		if region.ActionID == actionID && region.Rect == rect {
			return true
		}
	}
	return false
}

func TestChromePrimitiveToastSpecPreservesCurrentGeometry(t *testing.T) {
	rect := Rect{X: 20, Y: 3, W: 42, H: 5}
	primitive := ToastChromePrimitive(ToastVM{ID: "toast-1", Title: "pane.split"}, rect)

	if primitive.Rect != rect || primitive.Layer != LayerToast || primitive.Style != StyleToast {
		t.Fatalf("toast primitive must preserve current geometry/style, got %#v", primitive)
	}
	if primitive.ContentRect != (Rect{X: 22, Y: 5, W: 38, H: 1}) {
		t.Fatalf("toast text rect should match current centered toast geometry, got %#v", primitive.ContentRect)
	}
}
