package modal

import "github.com/lozzow/termx/tuiv2/input"

type CreateTargetKind string

const (
	CreateTargetReplace  CreateTargetKind = "replace"
	CreateTargetSplit    CreateTargetKind = "split"
	CreateTargetNewTab   CreateTargetKind = "new-tab"
	CreateTargetFloating CreateTargetKind = "floating"
)

type PromptState struct {
	Kind         string
	Title        string
	Hint         string
	Value        string
	Cursor       int
	AllowEmpty   bool
	Original     string
	PaneID       string
	TerminalID   string
	Command      []string
	DefaultName  string
	Name         string
	Tags         map[string]string
	CreateTarget CreateTargetKind
	ReturnMode   input.ModeKind
}
