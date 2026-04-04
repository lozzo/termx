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
	Workdir      string
	DefaultName  string
	Name         string
	Tags         map[string]string
	CreateTarget CreateTargetKind
	ReturnMode   input.ModeKind
	Fields       []PromptField
	ActiveField  int
}

type PromptField struct {
	Key         string
	Label       string
	Value       string
	Cursor      int
	Required    bool
	Placeholder string
}

func (p *PromptState) IsForm() bool {
	return p != nil && len(p.Fields) > 0
}

func (p *PromptState) Field(key string) *PromptField {
	if p == nil {
		return nil
	}
	for i := range p.Fields {
		if p.Fields[i].Key == key {
			return &p.Fields[i]
		}
	}
	return nil
}

func (p *PromptState) ActivePromptField() *PromptField {
	if p == nil || len(p.Fields) == 0 {
		return nil
	}
	if p.ActiveField < 0 {
		p.ActiveField = 0
	}
	if p.ActiveField >= len(p.Fields) {
		p.ActiveField = len(p.Fields) - 1
	}
	return &p.Fields[p.ActiveField]
}
