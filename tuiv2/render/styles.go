package render

import "github.com/charmbracelet/lipgloss"

var (
	terminalPickerTitleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f8fafc")).Background(lipgloss.Color("#0f172a")).Bold(true)
	terminalPickerQueryStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#dbeafe")).Background(lipgloss.Color("#0b1220")).Bold(true)
	terminalPickerBodyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0")).Background(lipgloss.Color("#020617"))
	pickerBorderStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff"))
	pickerFooterStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("#cbd5e1")).Background(lipgloss.Color("#0b1220"))
	pickerLineStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("#cbd5e1")).Background(lipgloss.Color("#0b1220"))
	pickerSelectedLineStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#020617")).Background(lipgloss.Color("#cbd5e1")).Bold(true)
	pickerCreateRowStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#d1fae5")).Background(lipgloss.Color("#123524")).Bold(true)
)
