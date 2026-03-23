package tui

import "strings"

type iconSet struct {
	Name      string
	Workspace string
	Pane      string
	Terminal  string
	Floating  string
	Running   string
	Exited    string
	Killed    string
	Waiting   string
	Unbound   string
	Owner     string
	Follower  string
	Fit       string
	Fixed     string
	Observer  string
	Readonly  string
	Pinned    string
	Shared    string
	LockWarn  string
	AutoFit   string
	CatchUp   string
	Notice    string
	Error     string
}

func normalizeIconSetName(name string) string {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "", "unicode":
		return "unicode"
	case "ascii":
		return "ascii"
	case "nerd":
		return "nerd"
	default:
		return "unicode"
	}
}

func resolveIconSet(name string) iconSet {
	switch normalizeIconSetName(name) {
	case "ascii":
		return iconSet{
			Name:      "ascii",
			Workspace: "ws",
			Pane:      "pane",
			Terminal:  "term",
			Floating:  "float",
			Running:   "run",
			Exited:    "exit",
			Killed:    "kill",
			Waiting:   "wait",
			Unbound:   "slot",
			Owner:     "owner",
			Follower:  "follow",
			Fit:       "fit",
			Fixed:     "fixed",
			Observer:  "obs",
			Readonly:  "ro",
			Pinned:    "pin",
			Shared:    "share",
			LockWarn:  "lock",
			AutoFit:   "auto-fit",
			CatchUp:   "sync",
			Notice:    "note",
			Error:     "err",
		}
	case "nerd":
		return iconSet{
			Name:      "nerd",
			Workspace: "󱂬",
			Pane:      "󰯉",
			Terminal:  "",
			Floating:  "󰆍",
			Running:   "",
			Exited:    "",
			Killed:    "󰅙",
			Waiting:   "󱞁",
			Unbound:   "󱞊",
			Owner:     "󰒃",
			Follower:  "󰈈",
			Fit:       "󰹑",
			Fixed:     "󰆾",
			Observer:  "󰈈",
			Readonly:  "󰌾",
			Pinned:    "󰐃",
			Shared:    "󰆧",
			LockWarn:  "󰌾",
			AutoFit:   "󰹑",
			CatchUp:   "󰁞",
			Notice:    "󰋽",
			Error:     "󰅚",
		}
	default:
		return iconSet{
			Name:      "unicode",
			Workspace: "⌂",
			Pane:      "▣",
			Terminal:  "⌁",
			Floating:  "◫",
			Running:   "●",
			Exited:    "○",
			Killed:    "✕",
			Waiting:   "…",
			Unbound:   "◌",
			Owner:     "◆",
			Follower:  "◇",
			Fit:       "⇄",
			Fixed:     "↔",
			Observer:  "◌",
			Readonly:  "🔒",
			Pinned:    "📌",
			Shared:    "⧉",
			LockWarn:  "⚠",
			AutoFit:   "⇄",
			CatchUp:   "↻",
			Notice:    "ℹ",
			Error:     "!",
		}
	}
}

func (s iconSet) token(label, icon string) string {
	if s.Name == "ascii" {
		return label
	}
	if icon == "" {
		return label
	}
	if label == "" {
		return icon
	}
	return icon + " " + label
}

func (s iconSet) countToken(label, icon string, value int) string {
	if s.Name == "ascii" {
		return label + ":" + itoa(value)
	}
	if icon == "" {
		return label + " " + itoa(value)
	}
	return icon + " " + itoa(value)
}

func (s iconSet) pairToken(label, icon, value string) string {
	if s.Name == "ascii" {
		if label == "" {
			return value
		}
		return label + ":" + value
	}
	if icon == "" {
		if label == "" {
			return value
		}
		return label + " " + value
	}
	if value == "" {
		return icon
	}
	return icon + " " + value
}
