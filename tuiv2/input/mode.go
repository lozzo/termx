package input

import "time"

type ModeKind string

const (
	ModeNormal          ModeKind = "normal"
	ModePrefix          ModeKind = "prefix"
	ModePane            ModeKind = "pane"
	ModeResize          ModeKind = "resize"
	ModeTab             ModeKind = "tab"
	ModeWorkspace       ModeKind = "workspace"
	ModeFloating        ModeKind = "floating"
	ModeDisplay         ModeKind = "display"
	ModeGlobal          ModeKind = "global"
	ModePicker          ModeKind = "picker"
	ModePrompt          ModeKind = "prompt"
	ModeHelp            ModeKind = "help"
	ModeTerminalManager ModeKind = "terminal-manager"
	ModeWorkspacePicker ModeKind = "workspace-picker"
)

type ModeState struct {
	Kind      ModeKind // 唯一可写 mode 真相
	Deadline  time.Time
	RequestID string
}
