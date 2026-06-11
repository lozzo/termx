package render

import (
	"os"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestFrameworkRendersCardPanelShellAndContent(t *testing.T) {
	vm := RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live", Hint: "term-1"},
		Layout: LayoutVM{
			Viewport: Rect{W: 40, H: 12},
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell 🚀",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("你好 output")}},
			}},
		},
	}}

	result := NewRenderer(DefaultTheme()).RenderResult(vm)
	assertFrameSize(t, result, 40, 12)
	if !linesContain(result.Lines(), "main") || !linesContain(result.Lines(), "shell 🚀") || !linesContain(result.Lines(), paneChromeCloseActionText()) || !linesContain(result.Lines(), "你好 output") {
		t.Fatalf("expected shell, panel title and content, got %#v", result.Lines())
	}
	if linesContain(result.Lines(), paneChromeRunningGlyph()) || linesContain(result.Lines(), "⇄2") || linesContain(result.Lines(), "◆ owner") || linesContain(result.Lines(), "1/31") {
		t.Fatalf("pane chrome should not render premature status/meta tokens, got %#v", result.Lines())
	}
	if !linesContain(result.Lines(), "┌") || !linesContain(result.Lines(), "┐") || !linesContain(result.Lines(), "└") || !linesContain(result.Lines(), "┘") {
		t.Fatalf("expected square Unicode pane chrome, got %#v", result.Lines())
	}
	assertAllRowsWidth(t, result.Lines(), 40)
}

func TestFrameworkRendersTuiv2TerminalOwnerHeader(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell.Workspace.Tabs[0].Panes[0].Title = "123"
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-1", true))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView("pane-2", "term-1", 8, 80, 24, state.TerminalResizeRoleFollower, "surface", "view-2", false))
	root.Surface = state.TerminalSurfaceStore{TerminalID: "term-1", Ready: true, State: state.TerminalLiveAttached, Lines: []string{"ready"}}

	result := NewRenderer(DefaultTheme()).RenderResult(NewRenderVMBuilder().Build(root))
	lines := result.Lines()
	want := paneChromeBracketToken(paneChromeSizeLockGlyph()) + " 123"
	if !linesContain(lines, want) || !linesContain(lines, paneChromeRunningGlyph()) || !linesContain(lines, "⇄2") || !linesContain(lines, "◆ owner") {
		t.Fatalf("expected tuiv2 owner terminal header tokens, want %q got %#v", want, lines)
	}
	if linesContain(lines, "size:owner") || linesContain(lines, "active") {
		t.Fatalf("terminal header should use tuiv2 grammar instead of generic meta/state, got %#v", lines)
	}
}

func TestFrameworkRendersTuiv2FloatingTerminalHeader(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell()}
	var floatingResult state.FloatingCommandResult
	root.Shell, floatingResult = root.Shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "float-1",
		Pane:     state.PaneState{ID: "float-pane-1", Title: "123", Kind: state.PaneTerminalLive, TerminalID: "term-1"},
		Rect:     state.FloatingRect{X: 2, Y: 2, W: 92, H: 8},
	})
	if floatingResult.Status != state.FloatingCommandOK {
		t.Fatalf("create floating: %#v", floatingResult)
	}
	root.TerminalViews = root.TerminalViews.BindFloating(state.NewFloatingTerminalView("float-1", "float-pane-1", "term-1", 7, 80, 24, state.TerminalResizeRoleFollower, "surface", "view-float", false))
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 8, 80, 24, state.TerminalResizeRoleOwner, "surface", "view-pane", true))
	root.Surface = state.TerminalSurfaceStore{TerminalID: "term-1", Ready: true, State: state.TerminalLiveAttached, Lines: []string{"ready"}}

	result := NewRenderer(DefaultTheme()).RenderResult(NewRenderVMBuilder().Build(root))
	lines := result.Lines()
	if !linesContain(lines, paneChromeBracketToken(paneChromeSizeLockGlyph())+" 123") || !linesContain(lines, paneChromeRunningGlyph()) || !linesContain(lines, "⇄2") || !linesContain(lines, "◇ follow") {
		t.Fatalf("expected tuiv2 floating terminal header tokens, got %#v", lines)
	}
	if !linesContain(lines, paneChromeBracketToken("")+"─"+paneChromeBracketToken("")+"─"+paneChromeBracketToken(paneChromeZoomGlyph())+"─"+paneChromeBracketToken(paneChromeCloseGlyph())) {
		t.Fatalf("expected floating-specific action glyphs, got %#v", lines)
	}
}

func TestFrameworkRendersUnconnectedPaneWithoutChromeActionCluster(t *testing.T) {
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneEmpty
	root.Shell.Workspace.Tabs[0].Panes[0].TerminalID = ""
	root.Shell.Workspace.Tabs[0].Panes[0].Title = "unconnected"

	result := NewRenderer(DefaultTheme()).RenderResult(NewRenderVMBuilder().Build(root))
	lines := result.Lines()
	if !linesContain(lines, "unconnected") || !linesContain(lines, "► Attach existing terminal ◄") || !linesContain(lines, "[ Create new terminal ]") || !linesContain(lines, "[ Open terminal manager ]") || !linesContain(lines, "[ Close pane ]") {
		t.Fatalf("expected unconnected pane title and content CTAs, got %#v", lines)
	}
	if !styledLinesContainText(result.StyledLines(), "► Attach existing terminal ◄", StyleAccent) || !styledLinesContainText(result.StyledLines(), "[ Create new terminal ]", StyleSuccess) || !styledLinesContainText(result.StyledLines(), "[ Close pane ]", StyleDangerStrong) {
		t.Fatalf("expected empty pane action-specific styles, got %#v", result.StyledLines())
	}
	if !linesContain(lines, paneChromeBracketToken(paneChromeZoomGlyph())) || !linesContain(lines, paneChromeBracketToken(paneChromeSplitVerticalGlyph())) || !linesContain(lines, paneChromeBracketToken(paneChromeSplitHorizontalGlyph())) || !linesContain(lines, paneChromeBracketToken(paneChromeCloseGlyph())) {
		t.Fatalf("unconnected pane should keep still-available pane chrome actions, got %#v", lines)
	}
	if linesContain(lines, paneChromeBracketToken("")) || linesContain(lines, paneChromeBracketToken("")) {
		t.Fatalf("unconnected pane must not render floating-only chrome action cluster, got %#v", lines)
	}
}

func TestFrameworkRendersContinuousCardPaneVerticalBorders(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 24, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
	}})

	lines := result.Lines()
	assertColumnGlyphs(t, lines, 0, 1, 7, "│")
	assertColumnGlyphs(t, lines, 23, 1, 7, "│")
	assertAllRowsWidth(t, lines, 24)
}

func TestFrameworkUsesKnownViewportExactly(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Title: "narrow"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{
			Viewport: Rect{W: 12, H: 6},
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
			}},
		},
	}})

	assertFrameSize(t, result, 12, 6)
	assertAllRowsWidth(t, result.Lines(), 12)
}

func TestFrameworkRendersSplitLineHorizontalAndVertical(t *testing.T) {
	panels := []PanelVM{
		{ID: "pane-1", Title: "shell", Presentation: PanelPresentationSplitLine, Active: false, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("left")}}},
		{ID: "pane-2", Title: "logs", Presentation: PanelPresentationSplitLine, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("right")}}},
	}
	horizontal := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: panels, Split: SplitVM{Direction: SplitHorizontal, Children: []SplitVM{{PaneID: "pane-1"}, {PaneID: "pane-2"}}}},
	}})
	vertical := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: panels, Split: SplitVM{Direction: SplitVertical, Children: []SplitVM{{PaneID: "pane-1"}, {PaneID: "pane-2"}}}},
	}})

	if !linesContain(horizontal.Lines(), "right") || !linesContain(vertical.Lines(), "right") {
		t.Fatalf("expected active panel content in both split modes")
	}
	assertColumnGlyphs(t, vertical.Lines(), 20, 0, 12, "│┬┴┼")
	assertColumnGlyphs(t, vertical.Lines(), 39, 0, 12, "│┐┘┤")
	if !linesContain(horizontal.Lines(), "logs") || !linesContain(horizontal.Lines(), paneChromeCloseActionText()) || !linesContain(horizontal.Lines(), "├") {
		t.Fatalf("expected horizontal split chrome/separator, got %#v", horizontal.Lines())
	}
	if linesContain(horizontal.Lines(), paneChromeRunningGlyph()) || linesContain(horizontal.Lines(), "⇄2") || linesContain(horizontal.Lines(), "◆ owner") || linesContain(horizontal.Lines(), "1/31") {
		t.Fatalf("split pane chrome should not render premature status/meta tokens, got %#v", horizontal.Lines())
	}
	assertAllRowsWidth(t, horizontal.Lines(), 40)
	assertAllRowsWidth(t, vertical.Lines(), 40)
}

func TestFrameworkRendersSplitLineTopBoundaryWithChromeOverlay(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 42, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell 🚀",
			Presentation: PanelPresentationSplitLine,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("你好 output")}},
		}}},
	}})
	lines := result.Lines()

	if got := SliceCells(lines[0], 0, 1); got != "┌" {
		t.Fatalf("split-line top boundary should keep top-left corner, got %q frame=%#v", got, lines)
	}
	if got := SliceCells(lines[0], 41, 42); got != "┐" {
		t.Fatalf("split-line top boundary should keep top-right corner, got %q frame=%#v", got, lines)
	}
	if !strings.Contains(lines[0], " shell 🚀 ") {
		t.Fatalf("split-line title slot should keep the remaining top border, got %#v", lines[0])
	}
	if !strings.Contains(lines[0], paneChromeActionText(42)) {
		t.Fatalf("split-line title/action slots should keep the remaining top border, got %#v", lines[0])
	}
	if strings.Contains(lines[0], paneChromeRunningGlyph()) || strings.Contains(lines[0], "⇄2") || strings.Contains(lines[0], "◆ owner") || strings.Contains(lines[0], "1/31") {
		t.Fatalf("split-line top chrome should not render premature status/meta tokens, got %#v", lines[0])
	}
	if !linesContain(lines, "你好 output") {
		t.Fatalf("expected split-line content, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 42)
}

func TestFrameworkRendersSplitLineAsSharedOuterFrame(t *testing.T) {
	panels := []PanelVM{
		{ID: "left", Title: "shell", Presentation: PanelPresentationSplitLine, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("left body")}}},
		{ID: "right", Title: "logs", Presentation: PanelPresentationSplitLine, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("right body")}}},
	}
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 48, H: 10}, Panels: panels, Split: SplitVM{Direction: SplitVertical, Children: []SplitVM{{PaneID: "left"}, {PaneID: "right"}}}},
	}})
	lines := result.Lines()

	if SliceCells(lines[0], 0, 1) != "┌" || SliceCells(lines[0], 24, 25) != "┬" || SliceCells(lines[0], 47, 48) != "┐" {
		t.Fatalf("split-line top should compose shared outer frame and divider, got %#v", lines[0])
	}
	if SliceCells(lines[9], 0, 1) != "└" || SliceCells(lines[9], 24, 25) != "┴" || SliceCells(lines[9], 47, 48) != "┘" {
		t.Fatalf("split-line bottom should compose shared outer frame and divider, got %#v", lines[9])
	}
	assertColumnGlyphs(t, lines, 0, 1, 9, "│")
	assertColumnGlyphs(t, lines, 24, 1, 9, "│")
	assertColumnGlyphs(t, lines, 47, 1, 9, "│")
	if !strings.Contains(lines[1], "│left body") || !strings.Contains(lines[1], "│right body") {
		t.Fatalf("split-line content must stay inside frame and divider, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 48)
}

func TestFrameworkPreservesPaneChromeLineBetweenTitleAndAction(t *testing.T) {
	for _, presentation := range []PanelPresentation{PanelPresentationCard, PanelPresentationSplitLine} {
		result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
			Layout: LayoutVM{Viewport: Rect{W: 44, H: 8}, Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: presentation,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
			}}},
		}})
		line := result.Lines()[0]
		if !strings.Contains(line, " shell ") {
			t.Fatalf("presentation=%s should render title slot, got row=%q", presentation, line)
		}
		actionCol := cellIndex(line, paneChromeCloseActionText())
		if actionCol < 0 {
			t.Fatalf("presentation=%s missing visible close action in %#v", presentation, result.Lines())
		}
		if strings.Contains(line, "⇄2") || strings.Contains(line, "◆ owner") || strings.Contains(line, "1/31") {
			t.Fatalf("presentation=%s should not render premature pane meta slots, got row=%q", presentation, line)
		}
		if !strings.Contains(line, paneChromeActionText(44)) {
			t.Fatalf("presentation=%s should render wireframe action cluster, got row=%q", presentation, line)
		}
		assertAllRowsWidth(t, result.Lines(), 44)
	}
}

func TestFrameworkUsesUnicodeChromeAndNoDefaultASCIIBorders(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{Viewport: Rect{W: 42, H: 12}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell 🚀",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("你好 e\u0301 output")}},
		}}},
		Overlay: OverlayVM{Kind: OverlayTerminalPicker, Content: ContentVM{Kind: ContentTerminalPicker, Lines: []Line{NewLine("picker 世界")}}},
		Toasts:  []ToastVM{{ID: "toast-1", Severity: ToastInfo, Title: "notice 🚀", Body: "世界"}},
	}})

	lines := result.Lines()
	if !linesContain(lines, "┌") || !linesContain(lines, "┐") || !linesContain(lines, "└") || !linesContain(lines, "┘") || !linesContain(lines, "│") {
		t.Fatalf("expected square Unicode card/overlay/toast chrome, got %#v", lines)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "+|-") {
			t.Fatalf("default UI chrome must not contain ASCII + - or |, got %q", line)
		}
	}
	assertAllRowsWidth(t, lines, 42)
}

func TestFrameworkStylesActiveAndInactivePaneChromeDifferently(t *testing.T) {
	panels := []PanelVM{
		{ID: "pane-1", Title: "shell 🚀", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("left")}}},
		{ID: "pane-2", Title: "logs 世界", Presentation: PanelPresentationCard, Active: false, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("right")}}},
	}
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{
			Viewport: Rect{W: 54, H: 10},
			Panels:   panels,
			Split:    SplitVM{Direction: SplitVertical, Children: []SplitVM{{PaneID: "pane-1"}, {PaneID: "pane-2"}}},
		},
	}})
	frame := result.Frame()

	if !styledLinesContain(frame.StyledLines, "┌", StyleAccent) || !styledLinesContain(frame.StyledLines, "┐", StyleAccent) {
		t.Fatalf("active card pane border should use accent style, got %#v", frame.StyledLines)
	}
	if !styledLinesContain(frame.StyledLines, "┌", StyleMuted) || !styledLinesContain(frame.StyledLines, "┐", StyleMuted) {
		t.Fatalf("inactive card pane border should use muted style, got %#v", frame.StyledLines)
	}
	if !linesContain(frame.ANSILines, "\x1b[1;38;2;169;112;255m") || !linesContain(frame.ANSILines, "\x1b[38;2;119;113;127m") {
		t.Fatalf("pane chrome should output active accent and inactive muted SGR, got %#v", frame.ANSILines)
	}
	assertAllRowsWidth(t, frame.Lines, 54)
	if right := SliceCells(frame.Lines[1], 53, 54); right != "┐" && right != "│" {
		t.Fatalf("wide title should not break right pane border, got %#v", frame.Lines)
	}
}

func TestFrameworkRendersStyledTopAndBottomBars(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Workspace: "main", Tab: "1", ActivePane: "pane-1", TerminalSummary: "term:1", FloatingSummary: "float:0", Notice: "ok"},
		Footer: FooterVM{Visible: true, Mode: "live", Hint: "term-1", Actions: []string{"^P PANE", "^R RESIZE"}, ActiveTarget: "pane:shell term:term-1", GlobalSummary: "ws:main float:0 terminals:1"},
		Layout: LayoutVM{Viewport: Rect{W: 120, H: 10}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
	}})
	frame := result.Frame()

	if strings.HasPrefix(frame.Lines[0], "┌") || strings.HasSuffix(frame.Lines[0], "┐") || strings.Contains(frame.Lines[0], "─┬─") {
		t.Fatalf("top bar should be a product bar, not an outer wireframe, got %#v", frame.Lines[0])
	}
	if !strings.Contains(frame.Lines[0], "  main") || !strings.Contains(frame.Lines[0], "▎ 1 1 ") || !strings.Contains(frame.Lines[0], " ") || !strings.Contains(frame.Lines[0], "pane:pane-1") || !strings.Contains(frame.Lines[0], "! ok") {
		t.Fatalf("top bar should contain tuiv2-like workspace/tab/create/notice slots, got %#v", frame.Lines[0])
	}
	if strings.Contains(frame.Lines[0], "[＋]") || strings.Contains(frame.Lines[0], "[ ]") || strings.Contains(frame.Lines[0], "1:1") || strings.Contains(frame.Lines[0], "×") {
		t.Fatalf("top bar should not keep old bracket/indicator tokens, got %#v", frame.Lines[0])
	}
	footer := frame.Lines[len(frame.Lines)-1]
	if strings.HasPrefix(footer, "└") || strings.HasSuffix(footer, "┘") {
		t.Fatalf("bottom bar should be a product bar, not an outer wireframe, got %#v", footer)
	}
	if !strings.Contains(footer, "[Ctrl] • [P] PANE") || !strings.Contains(footer, "PANE • [R] RESIZE") || !strings.Contains(footer, "ws:main") || !strings.Contains(footer, "float:0") || !strings.Contains(footer, "terminals:1") {
		t.Fatalf("bottom bar should contain target wireframe action/summary tokens, got %#v", footer)
	}
	if strings.Contains(footer, "LIVE") || strings.Contains(footer, "[Ctrl+P]") || strings.Contains(footer, "»") || strings.Contains(footer, "term-1") || strings.Contains(footer, "● shell") {
		t.Fatalf("bottom bar should use status-bar metadata slots, got %#v", footer)
	}
	if !styledLinesContainText(frame.StyledLines[:1], "  main", StyleHeaderWorkspace) ||
		!styledLinesContainText(frame.StyledLines[:1], " ", StyleHeaderCreate) ||
		!styledLinesContainText(frame.StyledLines[:1], "! ok", StyleStatusWarning) ||
		!styledLinesContainText(frame.StyledLines[len(frame.StyledLines)-1:], "Ctrl", StyleFooterAccent) ||
		!styledLinesContainText(frame.StyledLines[len(frame.StyledLines)-1:], "PANE", StyleFooterMuted) {
		t.Fatalf("top/bottom bar cells should use status token styles, got %#v", frame.StyledLines)
	}
	if !strings.Contains(frame.ANSILines[0], "\x1b[48;2;8;8;13m") {
		t.Fatalf("top bar should output status background SGR, got %#v", frame.ANSILines[0])
	}
	if strings.Contains(frame.ANSILines[len(frame.ANSILines)-1], "\x1b[48;2;8;8;13m") {
		t.Fatalf("footer should not keep an overall status background SGR, got %#v", frame.ANSILines[len(frame.ANSILines)-1])
	}
	assertAllRowsWidth(t, frame.Lines, 120)
}

func TestFrameworkHeaderTabStylesMatchTuiv2Levels(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{
			Visible:   true,
			Workspace: "main",
			Tabs: []HeaderTabVM{
				{Index: 1, Title: "one", Active: false, CloseActionID: ActionTabClose.String(), CloseTargetID: "tab-one"},
				{Index: 2, Title: "two", Active: true, CloseActionID: ActionTabClose.String(), CloseTargetID: "tab-two"},
			},
		},
		Layout: LayoutVM{Viewport: Rect{W: 80, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
		}}},
	}})
	headerANSI := result.Frame().ANSILines[0]
	for _, want := range []string{
		"38;2;212;192;244", "48;2;60;46;85",
		"38;2;130;113;155", "48;2;8;8;13",
		"38;2;119;113;127", "38;2;171;111;119",
		"38;2;169;112;255", "48;2;42;34;59",
		"38;2;255;107;107", "48;2;43;54;71",
	} {
		if !strings.Contains(headerANSI, want) {
			t.Fatalf("header ANSI missing tuiv2-style SGR %s: %#v", want, headerANSI)
		}
	}
	if strings.Contains(headerANSI, "\x1b[2;38;2;119;113;127") {
		t.Fatalf("inactive tab should use muted foreground without dim SGR, got %#v", headerANSI)
	}
}

func TestFrameworkRendersFullFooterSummaryWhenWidthAllows(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Workspace: "main"},
		Footer: FooterVM{
			Visible:       true,
			Mode:          "live",
			ActionTokens:  footerActionCatalog("live"),
			GlobalSummary: "ws:main float:1 terminals:1",
		},
		Layout: LayoutVM{Viewport: Rect{W: 140, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
		}}},
	}})
	frame := result.Frame()
	footer := frame.Lines[len(frame.Lines)-1]
	if !strings.Contains(footer, "[G] GLOBAL") || !strings.Contains(footer, "ws:main float:1 terminals:1") {
		t.Fatalf("wide footer should keep full action strip and summary, got %#v", footer)
	}
	if strings.Contains(frame.ANSILines[len(frame.ANSILines)-1], "\x1b[48;2;8;8;13m") {
		t.Fatalf("wide footer should still have no status background, got %#v", frame.ANSILines[len(frame.ANSILines)-1])
	}
	assertAllRowsWidth(t, frame.Lines, 140)
}

func TestFrameworkRendersFullFooterSummaryAtVisualCompareWidth(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Workspace: "main"},
		Footer: FooterVM{
			Visible:       true,
			Mode:          "live",
			ActionTokens:  footerActionCatalog("live"),
			GlobalSummary: "ws:main float:1 terminals:1",
		},
		Layout: LayoutVM{Viewport: Rect{W: 120, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
		}}},
	}})
	frame := result.Frame()
	footer := frame.Lines[len(frame.Lines)-1]
	if !strings.Contains(footer, "[Ctrl]") || !strings.Contains(footer, "[P] PANE") || !strings.Contains(footer, "[G]") {
		t.Fatalf("120-col footer should keep shortcut strip endpoints, got %#v", footer)
	}
	if !strings.Contains(footer, "ws:main float:1 terminals:1") {
		t.Fatalf("120-col footer should keep full right summary, got %#v", footer)
	}
	if strings.Contains(frame.ANSILines[len(frame.ANSILines)-1], "\x1b[48;2;8;8;13m") {
		t.Fatalf("120-col footer should still have no status background, got %#v", frame.ANSILines[len(frame.ANSILines)-1])
	}
	assertAllRowsWidth(t, frame.Lines, 120)
}

func TestFrameworkCriticalFooterHintDoesNotRestoreStatusBackground(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{Visible: true, Workspace: "main"},
		Footer: FooterVM{
			Visible:       true,
			Mode:          "live",
			Hint:          "error: boom",
			ActiveTarget:  "pane:shell",
			ActionTokens:  footerActionCatalog("live"),
			GlobalSummary: "ws:main float:1 terminals:1",
		},
		Layout: LayoutVM{Viewport: Rect{W: 140, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
		}}},
	}})
	frame := result.Frame()
	footerANSI := frame.ANSILines[len(frame.ANSILines)-1]
	if !strings.Contains(frame.Lines[len(frame.Lines)-1], "error: boom") {
		t.Fatalf("critical footer should render hint text, got %#v", frame.Lines[len(frame.Lines)-1])
	}
	if strings.Contains(footerANSI, "\x1b[48;2;8;8;13m") {
		t.Fatalf("critical footer hint should not restore status background, got %#v", footerANSI)
	}
}

func TestFrameworkAppliesChromePatchesFromVM(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{
			Viewport: Rect{W: 32, H: 8},
			ChromePatches: []ChromePatchVM{
				{Anchor: ChromePatchAnchorBody, X: 2, Y: 1, Text: "patched", Style: StyleAccent, Owner: "test:patch", Layer: LayerChrome},
			},
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: PanelPresentationCard,
			}},
		},
	}})
	frame := result.Frame()
	if !strings.Contains(frame.Lines[1], "patched") {
		t.Fatalf("chrome patch should write relative to body, got %#v", frame.Lines)
	}
	if !styledLinesContainText(frame.StyledLines, "patched", StyleAccent) {
		t.Fatalf("chrome patch should keep VM style, got %#v", frame.StyledLines)
	}
}

func TestRenderFrameworkAndBuilderDoNotEmbedVisualAuditIDs(t *testing.T) {
	for _, file := range []string{"framework.go", "vm.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read render source %s: %v", file, err)
		}
		text := string(source)
		for _, forbidden := range []string{"visual-audit", "pane-logs", "float-visual"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s should consume explicit VM chrome data instead of visual-audit id %q", file, forbidden)
			}
		}
	}
}

func TestFrameworkRendersStructuredHeaderAndFooterTokens(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header: HeaderVM{
			Visible:   true,
			Workspace: "main",
			Tabs: []HeaderTabVM{
				{ID: "tab-shell", Title: "shell", Index: 1, CloseActionID: ActionTabClose.String()},
				{ID: "tab-build", Title: "build", Index: 2, Active: true, CloseActionID: ActionTabClose.String()},
			},
		},
		Footer: FooterVM{
			Visible: true,
			Mode:    "live",
			ActionTokens: []FooterActionVM{
				{Key: "^P", Label: "PANE", Style: StyleStatusAccent},
				{Key: "w", Label: "CLOSE", Style: StyleStatusWarning},
			},
			GlobalSummary: "ws:main tabs:2 panes:1 float:0",
		},
		Layout: LayoutVM{Viewport: Rect{W: 96, H: 10}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
		}}},
	}})
	frame := result.Frame()

	if !strings.Contains(frame.Lines[0], "1 shell ") || !strings.Contains(frame.Lines[0], "▎ 2 build ") {
		t.Fatalf("header should render structured tab slots, got %#v", frame.Lines[0])
	}
	footer := frame.Lines[len(frame.Lines)-1]
	if !strings.Contains(footer, "[Ctrl] • [P] PANE") || !strings.Contains(footer, "[w] CLOSE") || !strings.Contains(footer, "ws:main") || !strings.Contains(footer, "float:0") {
		t.Fatalf("footer should render structured action tokens, got %#v", footer)
	}
	if !styledLinesContainText(frame.StyledLines[len(frame.StyledLines)-1:], "w", StyleFooterKeyPicker) {
		t.Fatalf("footer should keep action token style from VM, got %#v", frame.StyledLines)
	}
	assertAllRowsWidth(t, frame.Lines, 96)
}

func TestFrameworkRendersStructuredPaneChromeSlots(t *testing.T) {
	panel := PanelVM{
		ID:           "pane-1",
		Presentation: PanelPresentationCard,
		Active:       true,
		Chrome: PanelChromeVM{
			Title: ChromeSlotVM{Text: "build", Style: StyleAccent},
			State: ChromeSlotVM{Text: "● active", Style: StyleSuccess},
			Meta:  []ChromeSlotVM{{Text: "80x24", Style: StyleMuted}},
			Actions: []ChromeActionVM{
				{Text: paneChromeSplitHorizontalActionText(), ActionID: ActionPaneSplitDown.String(), Style: StyleAccent},
				{Text: paneChromeSplitVerticalActionText(), ActionID: ActionPaneSplitRight.String(), Style: StyleAccent},
				{Text: paneChromeCloseActionText(), ActionID: ActionPaneClose.String(), Style: StyleAccent},
			},
		},
		Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
	}
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 72, H: 10}, Panels: []PanelVM{panel}},
	}})
	frame := result.Frame()
	if !strings.Contains(frame.Lines[0], "build") || !strings.Contains(frame.Lines[0], "● active") || !strings.Contains(frame.Lines[0], "80x24") {
		t.Fatalf("pane chrome should render structured title/state/meta slots, got %#v", frame.Lines[0])
	}
	if !styledLinesContainText(frame.StyledLines[:1], "● active", StyleSuccess) || !styledLinesContainText(frame.StyledLines[:1], "80x24", StyleMuted) {
		t.Fatalf("pane chrome structured slots should keep styles, got %#v", frame.StyledLines)
	}
	assertAllRowsWidth(t, frame.Lines, 72)
}

func TestFrameworkDropsPaneChromeMetaBeforeActionsOnNarrowPane(t *testing.T) {
	panel := PanelVM{
		ID:           "pane-1",
		Presentation: PanelPresentationCard,
		Active:       true,
		Chrome: PanelChromeVM{
			Title: ChromeSlotVM{Text: "long-title", Style: StyleAccent},
			State: ChromeSlotVM{Text: "● active", Style: StyleSuccess},
			Meta:  []ChromeSlotVM{{Text: "owner"}, {Text: "80x24"}},
			Actions: []ChromeActionVM{
				{Text: paneChromeSplitHorizontalActionText(), ActionID: ActionPaneSplitDown.String(), Style: StyleAccent},
				{Text: paneChromeSplitVerticalActionText(), ActionID: ActionPaneSplitRight.String(), Style: StyleAccent},
				{Text: paneChromeCloseActionText(), ActionID: ActionPaneClose.String(), Style: StyleAccent},
			},
		},
	}
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 20, H: 8}, Panels: []PanelVM{panel}},
	}})
	frame := result.Frame()
	if strings.Contains(frame.Lines[0], "owner") || strings.Contains(frame.Lines[0], "80x24") || strings.Contains(frame.Lines[0], "● active") {
		t.Fatalf("narrow pane should drop meta/state slots before action/title, got %#v", frame.Lines[0])
	}
	if !strings.Contains(frame.Lines[0], paneChromeCloseActionText()) {
		t.Fatalf("narrow pane should retain visible close action, got %#v", frame.Lines[0])
	}
	assertAllRowsWidth(t, frame.Lines, 20)
}

func TestFrameworkRendersFloatingLayerAboveTiledPane(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{
			Viewport: Rect{W: 64, H: 18},
			Panels: []PanelVM{{
				ID:           "pane-1",
				Title:        "shell",
				Presentation: PanelPresentationCard,
				Active:       true,
				Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("tiled background")}},
			}},
			Floating: []FloatingVM{{
				ID:      "float-1",
				Title:   "浮窗🚀",
				Rect:    Rect{X: 8, Y: 4, W: 36, H: 8},
				Z:       1,
				Active:  true,
				Content: ContentVM{Kind: ContentEmptyPane, Lines: []Line{NewLine("floating body 世界")}},
			}},
		},
	}})
	frame := result.Frame()

	if !linesContain(frame.Lines, paneChromeBracketToken(paneChromeZoomGlyph())+"─"+paneChromeBracketToken(paneChromeCloseGlyph())) || !linesContain(frame.Lines, "floating body 世界") {
		t.Fatalf("expected floating bracket actions/content, got %#v", frame.Lines)
	}
	if linesContain(frame.Lines, paneChromeRunningGlyph()+" float") || linesContain(frame.Lines, "──── ·") {
		t.Fatalf("floating chrome should not render old title/status strip, got %#v", frame.Lines)
	}
	if !styledLinesContain(frame.StyledLines, "┌", StyleAccent) || !styledLinesContain(frame.StyledLines, "┘", StyleAccent) {
		t.Fatalf("active floating border should use accent style, got %#v", frame.StyledLines)
	}
	if !styledLinesContainText(frame.StyledLines, paneChromeBracketToken(paneChromeZoomGlyph()), StyleAccent) || !styledLinesContainText(frame.StyledLines, paneChromeBracketToken(paneChromeCloseGlyph()), StyleAccent) {
		t.Fatalf("floating actions should keep active accent style, got %#v", frame.StyledLines)
	}
	layer := firstLayer(t, result, LayerFloating)
	if layer.Rect != (Rect{X: 8, Y: 4, W: 36, H: 8}) {
		t.Fatalf("unexpected floating layer rect %#v", layer)
	}
	assertAllRowsWidth(t, frame.Lines, 64)
}

func TestFrameworkRendersModeSpecificFooterHints(t *testing.T) {
	cases := []struct {
		name string
		mode string
		want string
	}{
		{name: "pane", mode: "pane", want: "[%] VSPLIT"},
		{name: "resize", mode: "resize", want: "[←/h]"},
		{name: "global", mode: "global", want: "[h] HEADER"},
		{name: "tab", mode: "tab", want: "[c] NEW"},
		{name: "workspace", mode: "workspace", want: "[x] DELETE"},
		{name: "copy", mode: "copy", want: "[PgUp] SCROLL"},
		{name: "overlay", mode: "terminal-picker", want: "[enter] SELECT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := NewRenderer(DefaultTheme()).Render(RenderVM{Shell: ShellVM{
				Header: HeaderVM{Visible: true, Workspace: "main"},
				Footer: FooterVM{Visible: true, Mode: tc.mode, Actions: footerActions(tc.mode), ActiveTarget: "pane:shell", GlobalSummary: "ws:main tabs:1 panes:1 float:0"},
				Layout: LayoutVM{Viewport: Rect{W: 96, H: 9}, Panels: []PanelVM{{
					ID:           "pane-1",
					Title:        "shell",
					Presentation: PanelPresentationCard,
					Active:       true,
					Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
				}}},
			}})
			footer := frame.Lines[len(frame.Lines)-1]
			if !strings.Contains(footer, strings.ToUpper(tc.mode)) || !strings.Contains(footer, tc.want) || strings.Contains(footer, "● shell") {
				t.Fatalf("footer missing mode-specific product hints for %s: %#v", tc.mode, footer)
			}
			assertAllRowsWidth(t, frame.Lines, 96)
		})
	}
}

func TestFrameworkComposesUnicodeSplitConnections(t *testing.T) {
	panels := []PanelVM{
		{ID: "left-top", Title: "lt", Presentation: PanelPresentationSplitLine, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("lt")}}},
		{ID: "left-bottom", Title: "lb", Presentation: PanelPresentationSplitLine, Content: ContentVM{Kind: ContentPlaceholder, Lines: []Line{NewLine("lb")}}},
		{ID: "right", Title: "right", Presentation: PanelPresentationSplitLine, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("right")}}},
	}
	split := SplitVM{Direction: SplitVertical, Children: []SplitVM{
		{Direction: SplitHorizontal, Children: []SplitVM{{PaneID: "left-top"}, {PaneID: "left-bottom"}}},
		{PaneID: "right"},
	}}

	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: panels, Split: split},
	}})

	lines := result.Lines()
	if SliceCells(lines[0], 20, 21) != "┬" || SliceCells(lines[6], 0, 1) != "├" || SliceCells(lines[6], 20, 21) != "┤" || SliceCells(lines[11], 20, 21) != "┴" {
		t.Fatalf("expected composed split divider joints, got %#v", lines)
	}
	if !linesContain(lines, paneChromeBracketToken(paneChromeCloseActionText())) {
		t.Fatalf("expected at least close action tokens in narrow split chrome, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 40)
}

func TestFrameworkPreservesStyledContentThroughMatrixAndANSIFrame(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 36, H: 10}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "pane",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{{Cells: []Cell{
				{Text: "accent", Width: 6, Style: StyleAccent, Safe: true},
				{Text: " plain", Width: 6, Safe: true},
				{Text: " dir", Width: 4, ANSIStyle: ANSICellStyle{FG: "ansi:4", Bold: true}, Safe: true},
			}}}},
		}}},
	}})
	frame := result.Frame()

	if !linesContain(frame.Lines, "accent plain dir") {
		t.Fatalf("plain snapshot should keep text without ANSI, got %#v", frame.Lines)
	}
	if !linesContain(frame.ANSILines, "\x1b[1;38;2;169;112;255m") || !linesContain(frame.ANSILines, ANSIReset) {
		t.Fatalf("ANSI frame should retain styled matrix cells and reset, got %#v", frame.ANSILines)
	}
	if !linesContain(frame.ANSILines, "\x1b[1;34md") || !linesContain(frame.ANSILines, "\x1b[1;34mr") || linesContain(frame.ANSILines, "38;2;122;184;255m") {
		t.Fatalf("terminal ANSI cell style should survive compositor as host palette code, got %#v", frame.ANSILines)
	}
	if !styledLinesContain(frame.StyledLines, "a", StyleAccent) {
		t.Fatalf("styled frame should retain StyleAccent cells, got %#v", frame.StyledLines)
	}
	if !styledLinesContainANSI(frame.StyledLines, " ", ANSICellStyle{FG: "ansi:4", Bold: true}) {
		t.Fatalf("styled frame should retain terminal ANSI cell style, got %#v", frame.StyledLines)
	}
	assertAllRowsWidth(t, frame.Lines, 36)
}

func TestCanvasMatrixTracksOwnerLayerContinuationAndSafeFlag(t *testing.T) {
	c := newCanvas(6, 1)
	c.writeTextStyled(0, 0, 2, "你", StyleAccent, "pane-1", LayerPanel)

	if cell := c.rows[0][0]; cell.text != "你" || cell.width != 2 || cell.style != StyleAccent || cell.owner != "pane-1" || cell.layer != LayerPanel || !cell.safe {
		t.Fatalf("unexpected matrix anchor cell %#v", cell)
	}
	if cell := c.rows[0][1]; !cell.continuation || cell.owner != "pane-1" || cell.layer != LayerPanel {
		t.Fatalf("expected wide-cell continuation footprint, got %#v", cell)
	}
}

func TestCanvasMatrixMaterializesEmojiCompensationBeforeDots(t *testing.T) {
	c := newCanvas(6, 1)
	c.writeLine(0, 0, 6, Line{Cells: []Cell{
		NewCell("x"),
		NewCell("🚀"),
		NewCell("···"),
	}}, "pane-1", LayerPanel)

	if cell := c.rows[0][2]; !cell.compensate || cell.continuation || cell.owner != "pane-1" || cell.layer != LayerPanel {
		t.Fatalf("expected emoji compensation footprint before dots, got %#v", cell)
	}
	line := c.lines()[0]
	if got := line.PlainString(); got != "x🚀···" || line.Width() != 6 {
		t.Fatalf("compensation must not change plain row width, got text=%q width=%d cells=%#v", got, line.Width(), line.Cells)
	}
	ansi := line.ANSIString(DefaultTheme())
	if !strings.Contains(ansi, "x🚀\x1b[4G···") {
		t.Fatalf("compensation should re-anchor before following dots, got %q", ansi)
	}
}

func TestCanvasMatrixMaterializesEmojiCompensationBeforeBorder(t *testing.T) {
	c := newCanvas(4, 1)
	c.writeLine(0, 0, 4, Line{Cells: []Cell{
		NewCell("🚀"),
		NewCell("│"),
		NewCell(" "),
	}}, "pane-1", LayerPanel)

	line := c.lines()[0]
	if got := line.PlainString(); got != "🚀│ " || line.Width() != 4 {
		t.Fatalf("compensation must keep border at model column 3, got text=%q width=%d cells=%#v", got, line.Width(), line.Cells)
	}
	ansi := line.ANSIString(DefaultTheme())
	if !strings.Contains(ansi, "🚀\x1b[3G│") {
		t.Fatalf("compensation should re-anchor before following border, got %q", ansi)
	}
}

func TestCanvasMatrixClearsWideCellFootprintBeforeOverwrite(t *testing.T) {
	c := newCanvas(6, 1)
	c.writeText(0, 0, 6, "你你你")
	c.writeText(1, 0, 1, "x")

	line := c.lines()[0].PlainString()
	if got := DisplayWidth(line); got != 6 {
		t.Fatalf("matrix overwrite must keep row width 6, got %d line=%q cells=%#v", got, line, c.rows[0])
	}
	if !strings.HasPrefix(line, " x") {
		t.Fatalf("overwrite at continuation cell should clear old wide footprint, got %q cells=%#v", line, c.rows[0])
	}
}

func TestFrameworkStripsRawANSIInputBeforeMatrixLayout(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 30, H: 8}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "pane",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("\x1b[31mred 世界\x1b[0m")}},
		}}},
	}})

	lines := result.Lines()
	if !linesContain(lines, "red 世界") {
		t.Fatalf("plain matrix output should strip raw ANSI and keep text, got %#v", lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "\x1b[") {
			t.Fatalf("plain matrix output must not contain raw ANSI, got %q", line)
		}
	}
	assertAllRowsWidth(t, lines, 30)
	if right := SliceCells(lines[1], 29, 30); right != "│" {
		t.Fatalf("right border should survive ANSI/wide content, got %#v", lines)
	}
}

func TestFrameworkHeaderFooterHideReclaimsBody(t *testing.T) {
	base := ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{Viewport: Rect{W: 30, H: 10}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
	}
	visible := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: base})
	base.Header.Visible = false
	base.Footer.Visible = false
	hidden := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: base})

	visiblePanel := firstLayer(t, visible, LayerPanel)
	hiddenPanel := firstLayer(t, hidden, LayerPanel)
	if hiddenPanel.Rect.H <= visiblePanel.Rect.H {
		t.Fatalf("expected hidden header/footer to reclaim body, visible=%#v hidden=%#v", visiblePanel.Rect, hiddenPanel.Rect)
	}
}

func TestFrameworkRendersToastAndTerminalPickerOverlay(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Header:  HeaderVM{Visible: true, Title: "main"},
		Footer:  FooterVM{Visible: true, Mode: "live"},
		Layout:  LayoutVM{Viewport: Rect{W: 50, H: 14}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
		Overlay: OverlayVM{Kind: OverlayTerminalPicker, Content: ContentVM{Kind: ContentTerminalPicker, Lines: []Line{NewLine("picker pending")}, Pending: true}},
		Toasts:  []ToastVM{{ID: "toast-1", Severity: ToastWarning, Title: "warn 🚀", Body: "世界", Pending: true}},
	}})

	if !linesContain(result.Lines(), "picker pending") {
		t.Fatalf("expected terminal picker overlay, got %#v", result.Lines())
	}
	for _, line := range result.Lines() {
		if strings.Contains(line, "terminal picker") && strings.Contains(line, "esc") {
			t.Fatalf("terminal picker border title should not render esc hint, got %#v", result.Lines())
		}
	}
	if linesContain(result.Lines(), "warn 🚀") {
		t.Fatalf("overlay should stay in foreground above toast, got %#v", result.Lines())
	}
	if firstLayer(t, result, LayerOverlay).Rect.W == 0 || layerExists(result, LayerToast) {
		t.Fatalf("expected overlay layer and hidden toast layer, got %#v", result.Layers)
	}
	if linesContain(result.ANSILines(), "\x1b[48;2;20;18;27m") {
		t.Fatalf("terminal picker inner area should not use gray overlay background ANSI, got %#v", result.ANSILines())
	}
	if !styledLinesContain(result.StyledLines(), "┌", StyleForeground) || !styledLinesContain(result.StyledLines(), "┐", StyleForeground) {
		t.Fatalf("terminal picker border should keep foreground-only style, got %#v", result.StyledLines())
	}
	if styledLinesContainStyle(result.StyledLines(), StyleOverlay) {
		t.Fatalf("modal should not use gray overlay background, got %#v", result.StyledLines())
	}
	assertAllRowsWidth(t, result.Lines(), 50)
}

func TestFrameworkToastDoesNotOverwritePaneTopChrome(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 64, H: 16}, Panels: []PanelVM{{
			ID:           "pane-1",
			Title:        "shell",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
		Toasts: []ToastVM{{ID: "toast-1", Severity: ToastInfo, Title: "pane.split", Body: "created"}},
	}})
	lines := result.Lines()

	if !strings.Contains(lines[0], "┌─ shell") {
		t.Fatalf("toast must not overwrite pane top chrome, got %#v", lines)
	}
	if !strings.Contains(lines[0], paneChromeCloseActionText()) {
		t.Fatalf("pane top chrome should keep visible action tokens, got %#v", lines)
	}
	if strings.Contains(lines[0], paneChromeRunningGlyph()) || strings.Contains(lines[0], "⇄2") || strings.Contains(lines[0], "◆ owner") || strings.Contains(lines[0], "1/31") {
		t.Fatalf("pane top chrome should not render premature status/meta tokens, got %#v", lines)
	}
	if strings.Contains(lines[0], "pane.split") {
		t.Fatalf("toast should start below pane top chrome, got %#v", lines)
	}
	if linesContain(lines, "pane.split") || linesContain(lines, "created") {
		t.Fatalf("toast should be hidden while preserving panel chrome, got %#v", lines)
	}
	assertAllRowsWidth(t, lines, 64)
}

func TestFrameworkToastDoesNotChangeBodyLayout(t *testing.T) {
	shell := ShellVM{
		Header: HeaderVM{Visible: true, Title: "main"},
		Footer: FooterVM{Visible: true, Mode: "live"},
		Layout: LayoutVM{Viewport: Rect{W: 50, H: 14}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
	}
	withoutToast := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: shell})
	shell.Toasts = []ToastVM{{ID: "toast-1", Severity: ToastInfo, Title: "notice"}}
	withToast := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: shell})

	if firstLayer(t, withoutToast, LayerPanel).Rect != firstLayer(t, withToast, LayerPanel).Rect {
		t.Fatalf("toast must not change panel layout without=%#v with=%#v", firstLayer(t, withoutToast, LayerPanel).Rect, firstLayer(t, withToast, LayerPanel).Rect)
	}
}

func TestFrameworkTranslatesContentHitRegionsAndCursor(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Cursor: Cursor{Visible: true, Row: 1, Col: 2, Shape: CursorShapeBlock},
		Layout: LayoutVM{Viewport: Rect{W: 30, H: 10}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{
			Kind:       ContentCopyHistory,
			Lines:      []Line{NewLine("row")},
			HitRegions: []HitRegion{{Kind: HitRegionHistoryRow, Rect: Rect{Y: 0, W: 10, H: 1}, LineID: 42}},
		}}}},
	}})

	if !result.Cursor.Visible || result.Cursor.Row != 1 || result.Cursor.Col != 2 {
		t.Fatalf("expected cursor passthrough, got %#v", result.Cursor)
	}
	var contentRegion HitRegion
	for _, region := range result.HitRegions {
		if region.Kind == HitRegionHistoryRow && region.LineID == 42 {
			contentRegion = region
			break
		}
	}
	if contentRegion.Kind != HitRegionHistoryRow || contentRegion.Rect.Y == 0 {
		t.Fatalf("expected translated content hit region, got %#v", result.HitRegions)
	}
}

func TestFrameworkOpaqueOverlayOwnsCursor(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Cursor: Cursor{Visible: true, Row: 1, Col: 2, Shape: CursorShapeBlock},
		Layout: LayoutVM{Viewport: Rect{W: 40, H: 12}, Panels: []PanelVM{{ID: "pane-1", Title: "pane", Presentation: PanelPresentationCard, Active: true, Content: ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}}}}},
		Overlay: OverlayVM{
			Kind:   OverlayPrompt,
			Opaque: true,
			Content: ContentVM{
				Kind:   ContentPrompt,
				Lines:  []Line{NewLine("prompt")},
				Cursor: Cursor{Visible: true, Row: 3, Col: 4, Shape: CursorShapeBar},
			},
		},
	}})

	if !result.Cursor.Visible || result.Cursor.Row != 3 || result.Cursor.Col != 4 || result.Cursor.Shape != CursorShapeBar {
		t.Fatalf("expected opaque overlay cursor, got %#v", result.Cursor)
	}
}

func TestFrameworkRendersOverlayPopupAbovePromptModal(t *testing.T) {
	result := NewRenderer(DefaultTheme()).RenderResult(RenderVM{Shell: ShellVM{
		Layout: LayoutVM{Viewport: Rect{W: 34, H: 10}, Panels: []PanelVM{{
			ID:           "pane-1",
			Presentation: PanelPresentationCard,
			Active:       true,
			Content:      ContentVM{Kind: ContentTerminalLive, Lines: []Line{NewLine("body")}},
		}}},
		Overlay: OverlayVM{
			Kind:   OverlayPrompt,
			Opaque: true,
			Content: ContentVM{
				Kind:  ContentPrompt,
				Lines: []Line{NewLine("◆ Create Terminal"), NewLine("name*: shell"), NewLine("command: codex"), NewLine("workdir: /tmp/de")},
			},
			Popup: OverlayPopupVM{
				Kind:      OverlayPopupPromptSuggestion,
				AnchorRow: 4,
				AnchorCol: 9,
				Lines: []Line{
					{Cells: []Cell{styledCell("  path: /tmp", StylePromptSuggestion)}},
					{Cells: []Cell{styledCell("▸ ", StylePromptSuggestionHit), styledCell("  ", StylePromptSuggestionHit), styledCell("dev/", StylePromptSuggestionHit)}},
				},
			},
		},
	}})

	if len(result.Layers) < 2 || result.Layers[len(result.Layers)-2].Kind != LayerOverlay || result.Layers[len(result.Layers)-1].Kind != LayerPopup {
		t.Fatalf("popup should be rendered after prompt overlay, layers=%#v", result.Layers)
	}
	if !strings.Contains(plainLines(result.Content), "dev/") {
		t.Fatalf("expected popup row to be visible above modal clipping, got %#v", result.Lines())
	}
}

func assertFrameSize(t *testing.T, result RenderResult, width int, height int) {
	t.Helper()
	if result.Metadata.Width != width || result.Metadata.Height != height || len(result.Content) != height {
		t.Fatalf("unexpected result size metadata=%#v lines=%d", result.Metadata, len(result.Content))
	}
}

func assertAllRowsWidth(t *testing.T, lines []string, width int) {
	t.Helper()
	for i, line := range lines {
		if got := DisplayWidth(line); got != width {
			t.Fatalf("row %d width=%d want=%d line=%q", i, got, width, line)
		}
	}
}

func linesContain(lines []string, value string) bool {
	for _, line := range lines {
		if strings.Contains(line, value) {
			return true
		}
	}
	return false
}

func styledLinesContain(lines []Line, value string, style StyleToken) bool {
	for _, line := range lines {
		for _, cell := range line.Cells {
			if cell.Text == value && cell.Style == style {
				return true
			}
		}
	}
	return false
}

func styledLinesContainStyle(lines []Line, style StyleToken) bool {
	for _, line := range lines {
		for _, cell := range line.Cells {
			if cell.Style == style {
				return true
			}
		}
	}
	return false
}

func styledLinesContainText(lines []Line, value string, style StyleToken) bool {
	for _, line := range lines {
		var styledText strings.Builder
		for _, cell := range line.Cells {
			if cell.Style == style {
				styledText.WriteString(cell.Text)
			}
		}
		if strings.Contains(styledText.String(), value) {
			return true
		}
	}
	return false
}

func styledLinesContainANSI(lines []Line, value string, style ANSICellStyle) bool {
	for _, line := range lines {
		for _, cell := range line.Cells {
			if cell.Text == value && cell.ANSIStyle == style && cell.Style == "" {
				return true
			}
		}
	}
	return false
}

func cellIndex(line string, needle string) int {
	for col := 0; col <= DisplayWidth(line)-DisplayWidth(needle); col++ {
		if SliceCells(line, col, col+DisplayWidth(needle)) == needle {
			return col
		}
	}
	return -1
}

func frameHasRowPrefix(lines []string, col int, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(SliceCells(line, col, col+DisplayWidth(prefix)), prefix) {
			return true
		}
	}
	return false
}

func assertColumnGlyphs(t *testing.T, lines []string, col int, startRow int, endRow int, allowed string) {
	t.Helper()
	for row := startRow; row < endRow; row++ {
		if row < 0 || row >= len(lines) {
			t.Fatalf("row %d out of frame bounds lines=%d", row, len(lines))
		}
		got := SliceCells(lines[row], col, col+1)
		if !strings.Contains(allowed, got) {
			t.Fatalf("expected continuous border glyph at row=%d col=%d got=%q allowed=%q frame=%#v", row, col, got, allowed, lines)
		}
	}
}

func firstLayer(t *testing.T, result RenderResult, kind LayerKind) Layer {
	t.Helper()
	for _, layer := range result.Layers {
		if layer.Kind == kind {
			return layer
		}
	}
	t.Fatalf("missing layer %s in %#v", kind, result.Layers)
	return Layer{}
}

func layerExists(result RenderResult, kind LayerKind) bool {
	for _, layer := range result.Layers {
		if layer.Kind == kind {
			return true
		}
	}
	return false
}
