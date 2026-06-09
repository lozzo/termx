package render

import "testing"

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

func TestChromePrimitiveFloatingActionSlotsMatchHitRegions(t *testing.T) {
	rect := Rect{X: 10, Y: 4, W: 30, H: 8}
	primitive := FloatingChromePrimitive(FloatingVM{ID: "float-1", Rect: rect, Active: true}, rect, StyleAccent)
	regions := appendFloatingActionRegions(nil, rect, "float-1", Rect{W: 80, H: 24})

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
