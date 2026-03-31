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
			{Key: "Ctrl+P", Action: "pane mode"},
			{Key: "Ctrl+R", Action: "resize mode"},
			{Key: "Ctrl+T", Action: "tab mode"},
			{Key: "Ctrl+W", Action: "workspace mode"},
			{Key: "Ctrl+O", Action: "floating mode"},
			{Key: "Ctrl+V", Action: "display mode"},
			{Key: "Ctrl+F", Action: "terminal picker"},
			{Key: "Ctrl+G", Action: "global mode"},
			{Key: "Pane: Ctrl+D/E/H/J/K/L/W", Action: "split / focus / close pane"},
			{Key: "Tab: Ctrl+T/N/P/W", Action: "new / next / prev / close tab"},
			{Key: "Display: Ctrl+U/Y/V", Action: "scroll up / down / zoom"},
			{Key: "Global: Ctrl+Q / Ctrl+T", Action: "quit / terminal manager"},
			{Key: "Esc", Action: "close current mode/modal"},
		},
	}
}
