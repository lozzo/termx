package modal

type HelpState struct {
	Bindings []HelpBinding
}

type HelpBinding struct {
	Key    string
	Action string
}

func DefaultHelp() *HelpState {
	return &HelpState{
		Bindings: []HelpBinding{
			{Key: "Ctrl+P", Action: "pane mode — h/j/k/l focus, %/\" split, z zoom, w close"},
			{Key: "Ctrl+R", Action: "resize mode — h/j/k/l small, H/J/K/L large, = balance, Space layout"},
			{Key: "Ctrl+T", Action: "tab mode — c new, n/p next/prev, w close"},
			{Key: "Ctrl+W", Action: "workspace mode — n new, d delete, f picker"},
			{Key: "Ctrl+O", Action: "floating mode — n new float"},
			{Key: "Ctrl+V", Action: "display mode — u/d scroll, z zoom"},
			{Key: "Ctrl+F", Action: "terminal picker"},
			{Key: "Ctrl+G", Action: "global mode — Ctrl+Q quit, Ctrl+T manager"},
			{Key: "Picker: Enter / Tab / Ctrl+E / Ctrl+K", Action: "attach / split attach / edit / kill"},
			{Key: "Manager: Enter / Ctrl+T / Ctrl+O / Ctrl+E / Ctrl+K", Action: "attach / new tab / float / edit / kill"},
			{Key: "All modes: Esc", Action: "exit mode / close modal"},
		},
	}
}
