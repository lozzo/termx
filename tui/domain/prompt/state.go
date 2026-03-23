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
}

func (s *State) OverlayKind() types.OverlayKind {
	return types.OverlayPrompt
}

func (s *State) CloneOverlayData() types.OverlayData {
	if s == nil {
		return nil
	}
	clone := *s
	return &clone
}

func (s *State) AppendInput(text string) {
	if s == nil || text == "" {
		return
	}
	s.Draft += text
}

func (s *State) BackspaceInput() {
	if s == nil || s.Draft == "" {
		return
	}
	_, size := utf8.DecodeLastRuneInString(s.Draft)
	s.Draft = s.Draft[:len(s.Draft)-size]
}
