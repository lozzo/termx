package prompt

import (
	"unicode/utf8"

	"github.com/lozzow/termx/tui/domain/types"
)

type Kind string

const (
	KindCreateWorkspace      Kind = "create_workspace"
	KindEditTerminalMetadata Kind = "edit_terminal_metadata"
)

// State 承载 prompt overlay 的最小业务语义。
// 当前先支持 workspace 创建和 terminal metadata 编辑两类 prompt。
type State struct {
	Kind       Kind
	Title      string
	TerminalID types.TerminalID
	Draft      string
	Fields     []Field
	Active     int
}

type Field struct {
	Key   string
	Label string
	Value string
}

func (s *State) OverlayKind() types.OverlayKind {
	return types.OverlayPrompt
}

func (s *State) CloneOverlayData() types.OverlayData {
	if s == nil {
		return nil
	}
	clone := *s
	if len(s.Fields) > 0 {
		clone.Fields = append([]Field(nil), s.Fields...)
	}
	return &clone
}

func (s *State) AppendInput(text string) {
	if s == nil || text == "" {
		return
	}
	if len(s.Fields) > 0 {
		s.Fields[s.activeIndex()].Value += text
		return
	}
	s.Draft += text
}

func (s *State) BackspaceInput() {
	if s == nil {
		return
	}
	if len(s.Fields) > 0 {
		index := s.activeIndex()
		if s.Fields[index].Value == "" {
			return
		}
		_, size := utf8.DecodeLastRuneInString(s.Fields[index].Value)
		s.Fields[index].Value = s.Fields[index].Value[:len(s.Fields[index].Value)-size]
		return
	}
	if s.Draft == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(s.Draft)
	s.Draft = s.Draft[:len(s.Draft)-size]
}

func (s *State) NextField() bool {
	if s == nil || len(s.Fields) < 2 {
		return false
	}
	s.Active = (s.activeIndex() + 1) % len(s.Fields)
	return true
}

func (s *State) PreviousField() bool {
	if s == nil || len(s.Fields) < 2 {
		return false
	}
	// 结构化 prompt 需要支持双向切换字段，这样 shell 层只管把按键翻译成 intent。
	s.Active = (s.activeIndex() - 1 + len(s.Fields)) % len(s.Fields)
	return true
}

func (s *State) SetActiveField(index int) bool {
	if s == nil || len(s.Fields) == 0 {
		return false
	}
	if index < 0 || index >= len(s.Fields) {
		return false
	}
	s.Active = index
	return true
}

func (s *State) ActiveValue() string {
	if s == nil {
		return ""
	}
	if len(s.Fields) > 0 {
		return s.Fields[s.activeIndex()].Value
	}
	return s.Draft
}

func (s *State) activeIndex() int {
	if len(s.Fields) == 0 {
		return 0
	}
	if s.Active < 0 || s.Active >= len(s.Fields) {
		return 0
	}
	return s.Active
}
