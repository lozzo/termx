package render

import "charm.land/lipgloss/v2"

var (
	terminalPickerBodyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e6edf8")).
				Background(lipgloss.Color("#040814"))

	terminalPickerTitleStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#f8fafc")).
					Background(lipgloss.Color("#172338")).
					Padding(0, 1).
					Bold(true)

	terminalPickerQueryStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#dbeafe")).
					Background(lipgloss.Color("#0b1324"))

	pickerBorderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#30425f")).
				Background(lipgloss.Color("#0b1324"))

	pickerFooterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9db0cf")).
				Background(lipgloss.Color("#0b1324"))

	pickerLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cfdbeb")).
			Background(lipgloss.Color("#0b1324"))

	pickerSelectedLineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f8fafc")).
				Background(lipgloss.Color("#17365d")).
				Bold(true)

	pickerCreateRowStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#dcfce7")).
				Background(lipgloss.Color("#153726")).
				Bold(true)

	overlayFieldPrefixStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8ed7ff")).
				Background(lipgloss.Color("#101a2e")).
				Bold(true)

	overlayFieldValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e2e8f0")).
				Background(lipgloss.Color("#101a2e"))

	overlayCardFillStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e2e8f0")).
				Background(lipgloss.Color("#0b1324"))

	overlaySectionTitleStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#fbbf24")).
					Background(lipgloss.Color("#0b1324")).
					Bold(true)

	overlayHelpKeyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#86efac")).
				Background(lipgloss.Color("#0b1324")).
				Bold(true)

	overlayHelpActionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#cbd5e1")).
				Background(lipgloss.Color("#0b1324"))

	overlayFooterKeyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f8fafc")).
				Background(lipgloss.Color("#223554")).
				Bold(true)

	overlayFooterTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#afc0da")).
				Background(lipgloss.Color("#101a2e"))

	overlayFooterPlainStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#afc0da")).
				Background(lipgloss.Color("#101a2e"))
)
