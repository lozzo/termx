package render

import (
	"strconv"
	"strings"
)

func paneChromeTerminalLabelSlots(panel PanelVM, borderStyle StyleToken, width int) []paneChromeTopSlot {
	terminal := panel.Chrome.Terminal
	right := paneChromeTerminalRightSlots(terminal, borderStyle)
	for len(right) > 0 && paneChromeSlotsWidth(right)+paneChromeTerminalMinimumTitleWidth(terminal) > width {
		right = right[:len(right)-1]
	}
	rightWidth := paneChromeSlotsWidth(right)
	titleWidth := maxInt(0, width-rightWidth)
	left := paneChromeTerminalLeftTitle(terminal, panel, titleWidth, borderStyle)
	slots := make([]paneChromeTopSlot, 0, 1+len(right))
	if strings.TrimSpace(left.text) != "" {
		slots = append(slots, left)
	}
	return append(slots, right...)
}

func paneChromeTerminalLeftTitle(terminal TerminalChromeVM, panel PanelVM, width int, borderStyle StyleToken) paneChromeTopSlot {
	if width <= 0 {
		return paneChromeTopSlot{}
	}
	title := strings.TrimSpace(terminal.Title.Text)
	if title == "" {
		title = paneChromeTitleSource(panel)
	}
	if title == "" {
		return paneChromeTopSlot{}
	}
	lock := paneChromeBracketToken(paneChromeSizeLockGlyph())
	text := lock + " " + title
	if width > 2 {
		text = TruncateCells(text, width-2)
		text = " " + text + " "
	} else {
		text = TruncateCells(text, width)
	}
	style := terminal.Title.Style
	if style == "" {
		style = paneChromeTitleStyle(panel, borderStyle)
	}
	return paneChromeTopSlot{text: text, style: style, priority: 0}
}

func paneChromeTerminalRightSlots(terminal TerminalChromeVM, borderStyle StyleToken) []paneChromeTopSlot {
	stateText := strings.TrimSpace(terminal.State.Text)
	if stateText == "" {
		stateText = paneChromeRunningGlyph()
	}
	stateStyle := paneChromeSlotStyle(terminal.State, borderStyle)
	ownerStyle := paneChromeSlotStyle(terminal.Owner, borderStyle)
	ownerText := strings.TrimSpace(terminal.Owner.Text)
	if ownerText == "" {
		ownerText = "◇ follow"
	}
	count := terminal.AttachCount
	if count < 1 {
		count = 1
	}
	return []paneChromeTopSlot{
		{text: " " + stateText + " ", style: stateStyle, priority: 2},
		{text: "⇄" + strconv.Itoa(count) + " ", style: borderStyle, priority: 3},
		{text: ownerText + " ", style: ownerStyle, priority: 4, actionID: terminalOwnerActionID(terminal)},
	}
}

func terminalOwnerActionID(terminal TerminalChromeVM) string {
	if terminal.TakeOwner {
		return ActionTerminalTakeResizeOwner.String()
	}
	return ""
}

func paneChromeTerminalMinimumTitleWidth(terminal TerminalChromeVM) int {
	if strings.TrimSpace(terminal.Title.Text) == "" {
		return 0
	}
	return 5
}
