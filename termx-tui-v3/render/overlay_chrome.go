package render

import "strings"

func renderOverlay(c *canvas, overlay OverlayVM, rect Rect, contentRect Rect) Layer {
	if overlay.Kind == OverlayNone || overlay.Content.Kind == "" || rect.W <= 0 || rect.H <= 0 {
		return Layer{}
	}
	primitive := OverlayChromePrimitive(overlay, rect, contentRect)
	if overlay.Content.Kind != ContentTerminalPicker {
		c.fillStyledRect(rect, primitive.Style, primitive.Owner, primitive.Layer)
	} else {
		c.fillStyledRect(overlayInnerRect(rect), StylePicker, primitive.Owner, primitive.Layer)
	}
	chromeStyle := primitive.Style
	titleStyle := StyleAccent
	titleAction := "esc"
	if overlay.Content.Kind == ContentTerminalPicker {
		chromeStyle = StyleForeground
		titleStyle = StyleForeground
		titleAction = ""
	}
	c.drawStyledBox(rect, squareBoxStyle, chromeStyle, primitive.Owner, primitive.Layer)
	state := primitive.State.Text
	if overlay.Content.Kind == ContentTerminalPicker {
		state = ""
	}
	renderChromeCardTitle(c, rect, primitive.Title.Text, state, titleAction, titleStyle, primitive.Owner, primitive.Layer)
	contentLines := renderContent(c, overlay.Content, contentRect)
	return Layer{Kind: LayerOverlay, Rect: rect, Lines: contentLines}
}

func overlayInnerRect(rect Rect) Rect {
	return Rect{X: rect.X + 1, Y: rect.Y + 1, W: maxInt(0, rect.W-2), H: maxInt(0, rect.H-2)}
}

func overlayTitle(kind OverlayKind) string {
	title := strings.TrimSpace(string(kind))
	if title == "" {
		return "overlay"
	}
	return strings.ReplaceAll(title, "-", " ")
}

func renderChromeCardTitle(c *canvas, rect Rect, title string, state string, action string, style StyleToken, owner string, layer LayerKind) {
	if rect.W < 4 || rect.H <= 0 {
		return
	}
	titleX := rect.X + 2
	if layer == LayerFloating {
		titleX = rect.X + 1
	}
	actionText := ""
	if action != "" {
		actionText = " " + action + " "
	}
	actionWidth := DisplayWidth(actionText)
	actionX := rect.X + rect.W - actionWidth - 1
	if actionText != "" && rect.W >= actionWidth+8 {
		c.overlayTextStyled(actionX, rect.Y, actionWidth, actionText, style, owner, layer)
	}
	titleLimit := rect.X + rect.W - 3
	if actionText != "" && actionX > titleX {
		titleLimit = actionX - 1
	}
	stateText := ""
	if state != "" {
		stateText = " · " + state + " "
	}
	if stateText != "" && rect.W >= 34 {
		stateWidth := DisplayWidth(stateText)
		stateX := titleLimit - stateWidth
		if stateX > titleX+DisplayWidth(title)+2 {
			c.overlayTextStyled(stateX, rect.Y, stateWidth, stateText, style, owner, layer)
			titleLimit = stateX - 1
		}
	}
	if titleLimit > titleX {
		c.overlayTextStyled(titleX, rect.Y, titleLimit-titleX, " "+title+" ", style, owner, layer)
	}
}

func overlayChromeState(overlay OverlayVM) string {
	if overlay.Content.Pending {
		return "… pending"
	}
	if overlay.Content.Error != "" {
		return "× error"
	}
	if overlay.Content.Empty {
		return "○ empty"
	}
	return "● open"
}

func renderToasts(c *canvas, toasts []ToastVM, rects []Rect) []Layer {
	if len(toasts) == 0 || len(rects) == 0 {
		return nil
	}
	layers := make([]Layer, 0, len(toasts))
	for i, rect := range rects {
		toastIndex := len(toasts) - 1 - i
		if toastIndex < 0 {
			break
		}
		if rect.H <= 0 {
			break
		}
		toast := toasts[toastIndex]
		primitive := ToastChromePrimitive(toast, rect)
		owner := primitive.Owner
		c.fillStyledRect(rect, primitive.Style, owner, primitive.Layer)
		if rect.W >= 2 {
			for y := rect.Y; y < rect.Y+rect.H; y++ {
				c.writeTextStyled(rect.X, y, 1, "│", StyleToastAccent, owner, LayerToast)
				c.writeTextStyled(rect.X+rect.W-1, y, 1, "│", StyleToastAccent, owner, LayerToast)
			}
		}
		if primitive.ContentRect.W > 0 && primitive.ContentRect.H > 0 {
			textRect := primitive.ContentRect
			c.writeTextStyled(textRect.X, textRect.Y, textRect.W, centerToastText(toastMessageLine(toast), textRect.W), StyleToast, owner, LayerToast)
		}
		layers = append(layers, Layer{Kind: LayerToast, Rect: rect})
	}
	return layers
}

func toastTitleLine(toast ToastVM) string {
	title := toast.Title
	if title == "" {
		title = string(toast.Severity)
	}
	if toast.Pending {
		title += " ..."
	}
	return title
}

func toastMessageLine(toast ToastVM) string {
	title := strings.TrimSpace(toast.Title)
	if title == "" {
		title = strings.TrimSpace(toast.Body)
	}
	if title == "" {
		title = string(toast.Severity)
	}
	if toast.Pending {
		title += " ..."
	}
	return title
}

func centerToastText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = TruncateCells(text, width)
	pad := width - DisplayWidth(text)
	if pad <= 0 {
		return text
	}
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + text + strings.Repeat(" ", right)
}

func paneChromeCloseActionText() string {
	return paneChromeCloseGlyph()
}

func paneChromeSplitHorizontalActionText() string {
	return paneChromeSplitHorizontalGlyph()
}

func paneChromeSplitVerticalActionText() string {
	return paneChromeSplitVerticalGlyph()
}

func toastBodyLine(toast ToastVM) string {
	if toast.Body == "" {
		return string(toast.Severity)
	}
	return string(toast.Severity) + "  " + toast.Body
}

func toastSeverityStyle(severity ToastSeverity) StyleToken {
	switch severity {
	case ToastSuccess:
		return StyleSuccess
	case ToastWarning:
		return StyleWarning
	case ToastError:
		return StyleDanger
	default:
		return StyleInfo
	}
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
