package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func setupSuggestionPrompt(t *testing.T) *Model {
	t.Helper()
	model := setupModel(t, modelOpts{})
	model.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: "prompt-1"}
	model.modalHost.Prompt = &modal.PromptState{
		Kind:        "create-terminal-form",
		ActiveField: 2,
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{
				Key:             "workdir",
				Label:           "workdir",
				Value:           "/tmp/de",
				Cursor:          len([]rune("/tmp/de")),
				SuggestionTitle: "path: /tmp",
				SuggestionItems: []string{"/tmp/demo/", "/tmp/dev/"},
			},
			{Key: "tags", Label: "tags", Value: "role=dev"},
		},
	}
	model.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: "prompt-1"})
	return model
}

// Tab 在 workdir 有建议时应先进入建议选择态。
func TestPromptSuggestionTabEntersFocusedMode(t *testing.T) {
	model := setupSuggestionPrompt(t)

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if !model.modalHost.Prompt.PromptSuggestionFocused {
		t.Fatal("tab should enter suggestion focus")
	}
	if got, want := model.modalHost.Prompt.ActiveField, 2; got != want {
		t.Fatalf("active field after tab = %d, want %d", got, want)
	}
}

// Down 键在有建议时仍应优先用于表单导航。
func TestPromptSuggestionDownMovesToNextField(t *testing.T) {
	model := setupSuggestionPrompt(t)

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.modalHost.Prompt.PromptSuggestionFocused {
		t.Fatal("down should not enter suggestion focus")
	}
	if got, want := model.modalHost.Prompt.ActiveField, 3; got != want {
		t.Fatalf("active field after down = %d, want %d", got, want)
	}
}

// 进入选择态后，Down 在建议列表中向下移动。
func TestPromptSuggestionFocusedDownMovesSelection(t *testing.T) {
	model := setupSuggestionPrompt(t)

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if !model.modalHost.Prompt.PromptSuggestionFocused {
		t.Fatal("tab should enter suggestion focus when suggestions are visible")
	}
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if got := model.modalHost.Prompt.PromptSuggestionSelected; got != 1 {
		t.Fatalf("expected selected = 1, got %d", got)
	}
}

// 进入选择态后，Enter 接受当前补全但不跳字段。
func TestPromptSuggestionFocusedEnterAccepts(t *testing.T) {
	model := setupSuggestionPrompt(t)

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.modalHost.Prompt.PromptSuggestionFocused {
		t.Fatal("enter should accept suggestion and return focus to input")
	}
	if got, want := model.modalHost.Prompt.ActiveField, 2; got != want {
		t.Fatalf("active field after enter = %d, want %d", got, want)
	}
	if got, want := model.modalHost.Prompt.Field("workdir").Value, "/tmp/dev/"; got != want {
		t.Fatalf("workdir after focused enter = %q, want %q", got, want)
	}
}

// 进入选择态后，Tab 接受当前补全并跳到下一个字段。
func TestPromptSuggestionFocusedTabAcceptsAndMovesToNextField(t *testing.T) {
	model := setupSuggestionPrompt(t)

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if model.modalHost.Prompt.PromptSuggestionFocused {
		t.Fatal("tab should leave suggestion focus")
	}
	if got, want := model.modalHost.Prompt.ActiveField, 3; got != want {
		t.Fatalf("active field after focused tab accept = %d, want %d", got, want)
	}
	if got, want := model.modalHost.Prompt.Field("workdir").Value, "/tmp/dev/"; got != want {
		t.Fatalf("workdir after focused tab accept = %q, want %q", got, want)
	}
}

// 进入选择态后，Esc 只退出建议态，不应关闭整个 modal。
func TestPromptSuggestionFocusedEscOnlyExitsFocus(t *testing.T) {
	model := setupSuggestionPrompt(t)

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if !model.modalHost.Prompt.PromptSuggestionFocused {
		t.Fatal("tab should enter suggestion focus")
	}
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.modalHost.Prompt.PromptSuggestionFocused {
		t.Fatal("esc should leave suggestion focus")
	}
	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePrompt {
		t.Fatalf("esc in suggestion focus should not close prompt modal, got %#v", model.modalHost.Session)
	}
}
