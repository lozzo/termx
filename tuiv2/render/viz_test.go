package render

import (
	"fmt"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/modal"
	"testing"
)

func TestVisualizeWorkdirPopup(t *testing.T) {
	prompt := &modal.PromptState{
		Kind:                     "create-terminal-form",
		Title:                    "Create Terminal",
		Hint:                     "name is required; command, workdir, tags are optional",
		ActiveField:              2,
		PromptSuggestionSelected: 1,
		Fields: []modal.PromptField{
			{Key: "name", Label: "name", Value: "shell", Required: true},
			{Key: "command", Label: "command", Value: "/bin/sh"},
			{Key: "workdir", Label: "workdir", Value: "/tmp/de", SuggestionTitle: "path: /tmp", SuggestionItems: []string{"/tmp/demo/", "/tmp/dev/", "/tmp/deployment/"}},
			{Key: "tags", Label: "tags", Value: "role=dev"},
		},
	}
	overlay := xansi.Strip(renderPromptOverlay(prompt, TermSize{Width: 100, Height: 30}))
	fmt.Println(overlay)
}
