package render

type ChromePrimitiveKind string

const (
	ChromePrimitivePane     ChromePrimitiveKind = "pane"
	ChromePrimitiveFloating ChromePrimitiveKind = "floating"
	ChromePrimitiveOverlay  ChromePrimitiveKind = "overlay"
	ChromePrimitiveToast    ChromePrimitiveKind = "toast"
)

type ChromePrimitive struct {
	Kind        ChromePrimitiveKind
	Rect        Rect
	ContentRect Rect
	Style       StyleToken
	Owner       string
	Layer       LayerKind
	Title       ChromeSlot
	State       ChromeSlot
	ActionSlots []ChromeSlot
}

type ChromeSlot struct {
	Rect     Rect
	Text     string
	Style    StyleToken
	ActionID string
}

func PaneChromePrimitive(panel PanelVM, rect Rect, style StyleToken) ChromePrimitive {
	primitive := ChromePrimitive{
		Kind:  ChromePrimitivePane,
		Rect:  rect,
		Style: style,
		Owner: "pane:" + panel.ID,
		Layer: LayerPanel,
	}
	for _, slot := range paneChromeTopSlots(rect, panel, style) {
		chromeSlot := ChromeSlot{
			Rect:  Rect{X: slot.x, Y: rect.Y, W: DisplayWidth(slot.text), H: 1},
			Text:  slot.text,
			Style: slot.style,
		}
		primitive.Title = chromeSlot
	}
	actionRect := paneActionRect(panel, rect)
	items := visiblePaneChromeActionItems(panel, rect.W)
	primitive.ActionSlots = chromeActionSlotsFromItems(items, actionRect, 1)
	return primitive
}

func FloatingChromePrimitive(floating FloatingVM, rect Rect, style StyleToken) ChromePrimitive {
	primitive := ChromePrimitive{
		Kind:        ChromePrimitiveFloating,
		Rect:        rect,
		ContentRect: floatingContentRect(rect, floating.Collapsed),
		Style:       style,
		Owner:       "floating:" + floating.ID,
		Layer:       LayerFloating,
	}
	actionRect := floatingActionRect(rect)
	primitive.ActionSlots = chromeActionSlotsFromItems(floatingChromeActionItems(rect.W), actionRect, 1)
	return primitive
}

func OverlayChromePrimitive(overlay OverlayVM, rect Rect, contentRect Rect) ChromePrimitive {
	return ChromePrimitive{
		Kind:        ChromePrimitiveOverlay,
		Rect:        rect,
		ContentRect: contentRect,
		Style:       StyleOverlay,
		Owner:       "overlay:" + string(overlay.Kind),
		Layer:       LayerOverlay,
		Title: ChromeSlot{
			Rect:  Rect{X: rect.X + 2, Y: rect.Y, W: maxInt(0, rect.W-4), H: 1},
			Text:  overlayTitle(overlay.Kind),
			Style: StyleAccent,
		},
		State: ChromeSlot{Text: overlayChromeState(overlay), Style: StyleAccent},
	}
}

func ToastChromePrimitive(toast ToastVM, rect Rect) ChromePrimitive {
	return ChromePrimitive{
		Kind:        ChromePrimitiveToast,
		Rect:        rect,
		ContentRect: toastContentRect(rect),
		Style:       StyleToast,
		Owner:       "toast:" + toast.ID,
		Layer:       LayerToast,
	}
}

func chromeActionSlotsFromItems(items []paneChromeActionItem, rect Rect, gap int) []ChromeSlot {
	if len(items) == 0 || rect.W <= 0 || rect.H <= 0 {
		return nil
	}
	out := make([]ChromeSlot, 0, len(items))
	x := rect.X
	for index, item := range items {
		if index > 0 {
			x += gap
		}
		width := DisplayWidth(item.Text)
		if width <= 0 {
			continue
		}
		out = append(out, ChromeSlot{
			Rect:     Rect{X: x, Y: rect.Y, W: width, H: rect.H},
			Text:     item.Text,
			Style:    item.Style,
			ActionID: item.ActionID,
		})
		x += width
	}
	return out
}

func floatingContentRect(rect Rect, collapsed bool) Rect {
	if collapsed {
		return Rect{}
	}
	return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
}

func toastContentRect(rect Rect) Rect {
	if rect.H <= 0 || rect.W <= 4 {
		return Rect{}
	}
	return Rect{X: rect.X + 2, Y: rect.Y + rect.H/2, W: maxInt(0, rect.W-4), H: 1}
}
