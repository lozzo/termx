package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const overlayFooterActionGap = 2

type overlayFooterActionSpec struct {
	Label  string
	Action input.SemanticAction
}

type overlayFooterActionLayout struct {
	Label  string
	Action input.SemanticAction
	Rect   workbench.Rect
}

func pickerFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionPickerAttachSplit,
			input.ActionEditTerminal,
			input.ActionKillTerminal,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:      "attach",
			input.ActionPickerAttachSplit: "split+attach",
			input.ActionEditTerminal:      "edit",
			input.ActionKillTerminal:      "kill",
			input.ActionCancelMode:        "close",
		},
	)
}

func workspacePickerFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionCreateWorkspace,
			input.ActionRenameWorkspace,
			input.ActionDeleteWorkspace,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:    "open",
			input.ActionCreateWorkspace: "new",
			input.ActionRenameWorkspace: "rename",
			input.ActionDeleteWorkspace: "delete",
			input.ActionCancelMode:      "close",
		},
	)
}

func terminalManagerFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionAttachTab,
			input.ActionAttachFloating,
			input.ActionEditTerminal,
			input.ActionKillTerminal,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:   "here",
			input.ActionAttachTab:      "tab",
			input.ActionAttachFloating: "float",
			input.ActionEditTerminal:   "edit",
			input.ActionKillTerminal:   "kill",
			input.ActionCancelMode:     "close",
		},
	)
}

func promptFooterActionSpecs(prompt *modal.PromptState) []overlayFooterActionSpec {
	paneID := ""
	if prompt != nil {
		paneID = prompt.PaneID
	}
	return []overlayFooterActionSpec{
		{Label: "submit", Action: input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID}},
		{Label: "cancel", Action: input.SemanticAction{Kind: input.ActionCancelMode}},
	}
}

func floatingOverviewFooterActionSpecs() []overlayFooterActionSpec {
	return modeFooterActionSpecs(
		[]input.ActionKind{
			input.ActionSubmitPrompt,
			input.ActionExpandAllFloatingPanes,
			input.ActionCollapseAllFloatingPanes,
			input.ActionCloseFloatingPane,
			input.ActionCancelMode,
		},
		map[input.ActionKind]string{
			input.ActionSubmitPrompt:             "open",
			input.ActionExpandAllFloatingPanes:   "show-all",
			input.ActionCollapseAllFloatingPanes: "collapse-all",
			input.ActionCloseFloatingPane:        "close-pane",
			input.ActionCancelMode:               "close",
		},
	)
}

func modeFooterActionSpecs(order []input.ActionKind, fallback map[input.ActionKind]string) []overlayFooterActionSpec {
	specs := make([]overlayFooterActionSpec, 0, len(order))
	for _, kind := range order {
		label := strings.TrimSpace(fallback[kind])
		if strings.TrimSpace(label) == "" {
			continue
		}
		specs = append(specs, overlayFooterActionSpec{
			Label:  label,
			Action: input.SemanticAction{Kind: kind},
		})
	}
	return specs
}

func layoutOverlayFooterActions(specs []overlayFooterActionSpec, rowRect workbench.Rect) (string, []overlayFooterActionLayout) {
	return layoutOverlayFooterActionsWithTheme(defaultUITheme(), specs, rowRect)
}

func layoutOverlayFooterActionsWithTheme(theme uiTheme, specs []overlayFooterActionSpec, rowRect workbench.Rect) (string, []overlayFooterActionLayout) {
	if rowRect.W <= 0 || rowRect.H <= 0 || len(specs) == 0 {
		return "", nil
	}
	var builder strings.Builder
	actions := make([]overlayFooterActionLayout, 0, len(specs))
	currentX := 0
	for _, spec := range specs {
		label := renderOverlayFooterActionLabel(theme, spec.Label)
		labelW := xansi.StringWidth(label)
		if labelW <= 0 {
			continue
		}
		need := labelW
		if len(actions) > 0 {
			need += overlayFooterActionGap
		}
		if currentX+need > rowRect.W {
			break
		}
		if len(actions) > 0 {
			builder.WriteString(renderOverlaySpan(overlayFooterPlainStyle(theme), "", overlayFooterActionGap))
			currentX += overlayFooterActionGap
		}
		actions = append(actions, overlayFooterActionLayout{
			Label:  label,
			Action: spec.Action,
			Rect: workbench.Rect{
				X: rowRect.X + currentX,
				Y: rowRect.Y,
				W: labelW,
				H: 1,
			},
		})
		builder.WriteString(label)
		currentX += labelW
	}
	return builder.String(), actions
}

func renderOverlayFooterActionLabel(theme uiTheme, label string) string {
	key, text := splitOverlayFooterLabel(label)
	switch {
	case key != "" && text != "":
		return overlayFooterKeyStyle(theme).Render(key) + overlayFooterTextStyle(theme).Render(text)
	case key != "":
		return overlayFooterKeyStyle(theme).Render(key)
	default:
		return overlayFooterPlainStyle(theme).Render(label)
	}
}

func splitOverlayFooterLabel(label string) (string, string) {
	label = strings.TrimSpace(label)
	if !strings.HasPrefix(label, "[") {
		return "", label
	}
	end := strings.Index(label, "]")
	if end <= 0 {
		return "", label
	}
	key := label[:end+1]
	text := strings.TrimSpace(label[end+1:])
	if text != "" {
		text = " " + text
	}
	return key, text
}
