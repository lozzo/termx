package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	corev2 "github.com/lozzow/termx/termx-core-v2"
	"github.com/lozzow/termx/termx-tui-v3/app"
	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type v3E2ESmokeResult struct {
	TerminalID   string
	Frames       int
	ViewportCols int
	ViewportRows int
	SessionCols  int
	SessionRows  int
	CopyCols     int
	PaneCommands int
	PaneCount    int
	ActivePaneID string
	ZoomChecked  bool
}

func runV3E2ESmoke(ctx context.Context) (v3E2ESmokeResult, error) {
	socketDir, err := os.MkdirTemp("", "termx-v3-smoke-*")
	if err != nil {
		return v3E2ESmokeResult{}, err
	}
	defer os.RemoveAll(socketDir)
	socketPath := filepath.Join(socketDir, fmt.Sprintf("termx-v2-smoke-%d.sock", time.Now().UnixNano()))
	server := corev2.NewServer(
		corev2.WithSocketPath(socketPath),
		corev2.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe(serverCtx)
	}()
	defer func() {
		cancel()
		_ = server.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()
	if err := waitForSocket(socketPath, 2*time.Second, func() error {
		client, err := dialV3Client(socketPath)
		if err != nil {
			return err
		}
		return client.Close()
	}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	client, err := dialV3Client(socketPath)
	if err != nil {
		return v3E2ESmokeResult{}, err
	}
	defer client.Close()
	storageClient, err := dialV3Client(socketPath)
	if err != nil {
		return v3E2ESmokeResult{}, err
	}
	defer storageClient.Close()

	created, err := client.Create(ctx, protocol.CreateParams{
		ID:      newV3TerminalID(),
		Name:    "v3-e2e-smoke",
		Command: []string{"/bin/sh", "-c", "printf 'alpha\\nbeta\\n'; while IFS= read -r line; do printf 'echo:%s\\n' \"$line\"; done"},
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		return v3E2ESmokeResult{}, err
	}

	host := app.NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := newV3InteractiveRuntime(created.TerminalID, 80, 24, client, storageClient, host)
	if err := runtime.Post(app.LiveAttachMsg{Config: app.LiveConfig{
		TerminalID:   created.TerminalID,
		Cols:         80,
		Rows:         24,
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "v3-e2e-smoke",
		ViewID:       "v3-e2e-smoke-main",
	}}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if !runtime.State().Session.Attached {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: attach did not update tui-v3 state")
	}
	if got := runtime.State().Viewport; !got.Valid || got.Cols != 80 || got.Rows != 24 {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: initial host viewport was not ingested, got %#v", got)
	}
	if got := runtime.State().Session; got.Cols != 78 || got.Rows != 20 {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: initial attach did not use content rect, got %#v", got)
	}
	if err := drainV3RuntimeUntilFrameContains(ctx, runtime, host, "beta"); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if len(runtime.State().Surface.Lines) < 2 || runtime.State().Surface.Lines[0] != "alpha" || runtime.State().Surface.Lines[1] != "beta" {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: attach did not hydrate live surface rows, surface=%#v", runtime.State().Surface)
	}
	if !v3E2EFramesContain(host.Frames(), "alpha") || !v3E2EFramesContain(host.Frames(), "beta") {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: hydrated live rows were not rendered")
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "gamma"}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := drainV3RuntimeUntilFrameContains(ctx, runtime, host, "gamma"); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := validateV3E2EStyledChrome(host.Frames()); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := host.SendResize(100, 40); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if got := runtime.State().Viewport; !got.Valid || got.Cols != 100 || got.Rows != 40 {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: host resize viewport was not ingested, got %#v", got)
	}
	if got := runtime.State().Session; got.Cols != 98 || got.Rows != 36 {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: host resize did not resize terminal to content rect, got %#v", got)
	}
	if err := validateV3E2EFrameSize(host.Frames(), 100, 40); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := validateV3E2EStyledChrome(host.Frames()); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := runtime.Drain(ctx); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if !runtime.State().CopyMode.Active || len(runtime.State().History.Rows) == 0 {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: copy mode did not load authoritative history")
	}
	if runtime.State().CopyMode.BoundCols != 98 || runtime.State().History.Cols != 98 {
		return v3E2ESmokeResult{}, fmt.Errorf("v3 e2e smoke: copy mode did not bind resized content cols, state=%#v", runtime.State())
	}
	if err := validateV3E2EFrameSize(host.Frames(), 100, 40); err != nil {
		return v3E2ESmokeResult{}, err
	}
	paneCommands, zoomChecked, err := runV3E2EPaneCommands(ctx, runtime)
	if err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := validateV3E2EFrameSize(host.Frames(), 100, 40); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := client.Kill(ctx, created.TerminalID); err != nil {
		return v3E2ESmokeResult{}, err
	}
	if err := client.Remove(ctx, created.TerminalID); err != nil {
		return v3E2ESmokeResult{}, err
	}
	return v3E2ESmokeResult{
		TerminalID:   created.TerminalID,
		Frames:       len(host.Frames()),
		ViewportCols: runtime.State().Viewport.Cols,
		ViewportRows: runtime.State().Viewport.Rows,
		SessionCols:  runtime.State().Session.Cols,
		SessionRows:  runtime.State().Session.Rows,
		CopyCols:     runtime.State().CopyMode.BoundCols,
		PaneCommands: paneCommands,
		PaneCount:    len(runtime.State().Shell.EnsureDefaults().Workspace.Tabs[0].Panes),
		ActivePaneID: runtime.State().Shell.EnsureDefaults().ActivePaneID,
		ZoomChecked:  zoomChecked,
	}, nil
}

func runV3E2EPaneCommands(ctx context.Context, runtime *app.AppRuntime) (int, bool, error) {
	commands := []app.Msg{
		app.ShellPaneCommandMsg{Command: state.PaneCommand{
			Action:         state.PaneCommandSplit,
			SplitDirection: state.SplitDirectionVertical,
			NewPane:        state.PaneState{ID: "pane-e2e", Title: "e2e", Kind: state.PaneTerminalLive},
			Source:         state.PaneCommandSourceTest,
		}},
		app.ShellPaneCommandMsg{Command: state.PaneCommand{
			Action:          state.PaneCommandResize,
			Target:          state.PaneCommandTarget{PaneID: "pane-e2e"},
			ResizeDirection: state.PaneResizeLeft,
			Delta:           6,
			Source:          state.PaneCommandSourceTest,
		}},
		app.ShellPaneCommandMsg{Command: state.PaneCommand{
			Action: state.PaneCommandZoom,
			Target: state.PaneCommandTarget{PaneID: "pane-e2e"},
			Source: state.PaneCommandSourceTest,
		}},
	}
	for _, msg := range commands {
		if err := runtime.Post(msg); err != nil {
			return 0, false, err
		}
	}
	if err := runtime.Drain(ctx); err != nil {
		return 0, false, err
	}
	if runtime.State().Shell.ZoomedPaneID != "pane-e2e" {
		return 0, false, fmt.Errorf("v3 e2e smoke: zoom command did not set zoomed pane, shell=%#v", runtime.State().Shell)
	}
	if runtime.State().Session.Cols != 98 || runtime.State().Session.Rows != 36 {
		return 0, false, fmt.Errorf("v3 e2e smoke: zoom command should restore full card content rect, state=%#v", runtime.State())
	}
	if runtime.State().CopyMode.BoundCols != 98 || runtime.State().History.Cols != 98 {
		return 0, false, fmt.Errorf("v3 e2e smoke: zoom command should keep copy window rebound to content cols, state=%#v", runtime.State())
	}
	trailing := []app.Msg{
		app.ShellPaneCommandMsg{Command: state.PaneCommand{
			Action: state.PaneCommandUnzoom,
			Target: state.PaneCommandTarget{PaneID: "pane-e2e"},
			Source: state.PaneCommandSourceTest,
		}},
		app.ShellPaneCommandMsg{Command: state.PaneCommand{
			Action: state.PaneCommandClose,
			Target: state.PaneCommandTarget{PaneID: "pane-e2e"},
			Source: state.PaneCommandSourceTest,
		}},
	}
	for _, msg := range trailing {
		if err := runtime.Post(msg); err != nil {
			return 0, false, err
		}
	}
	if err := runtime.Drain(ctx); err != nil {
		return 0, false, err
	}
	shell := runtime.State().Shell.EnsureDefaults()
	if shell.ZoomedPaneID != "" || len(shell.Workspace.Tabs[0].Panes) != 1 || shell.ActivePaneID != state.DefaultPaneID {
		return 0, false, fmt.Errorf("v3 e2e smoke: close/unzoom command did not restore single-pane shell, shell=%#v", shell)
	}
	if runtime.State().Session.Cols != 98 || runtime.State().Session.Rows != 36 {
		return 0, false, fmt.Errorf("v3 e2e smoke: close command did not keep terminal at content rect, state=%#v", runtime.State())
	}
	return len(commands) + len(trailing), true, nil
}

// validateV3E2EFrameSize 固化非交互 smoke 的 UI 画布 contract：
// host viewport 已知时，最终 frame 必须逐行填满且不得触发宿主自动换行。
func validateV3E2EFrameSize(frames []render.Frame, cols int, rows int) error {
	if len(frames) == 0 {
		return fmt.Errorf("v3 e2e smoke: no frames rendered")
	}
	frame := frames[len(frames)-1]
	if len(frame.Lines) != rows {
		return fmt.Errorf("v3 e2e smoke: last frame rows=%d want=%d", len(frame.Lines), rows)
	}
	for row, line := range frame.Lines {
		if got := render.DisplayWidth(line); got != cols {
			return fmt.Errorf("v3 e2e smoke: last frame row %d width=%d want=%d", row, got, cols)
		}
	}
	return nil
}

// validateV3E2EStyledChrome 固定真实装配路径必须输出 styled chrome，
// 不能退回到裸 terminal 内容或纯文本占位。
func validateV3E2EStyledChrome(frames []render.Frame) error {
	if len(frames) == 0 {
		return fmt.Errorf("v3 e2e smoke: no frames rendered")
	}
	frame := frames[len(frames)-1]
	required := []string{" main ", "│ 1:main ×", "│ ＋ ", "┌─ shell", "ws:main"}
	for _, marker := range required {
		found := false
		for _, line := range frame.Lines {
			if strings.Contains(line, marker) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("v3 e2e smoke: styled chrome marker %q missing from frame %#v", marker, frame.Lines)
		}
	}
	glyphs := render.DefaultPaneChromeGlyphs()
	for _, marker := range []string{glyphs.Running, "◆ owner", "⇄2", "1/31"} {
		for _, line := range frame.Lines {
			if strings.Contains(line, "┌─") && strings.Contains(line, marker) {
				return fmt.Errorf("v3 e2e smoke: premature pane chrome marker %q present in frame %#v", marker, frame.Lines)
			}
		}
	}
	actionCluster := glyphs.SplitHorizontal + "  " + glyphs.SplitVertical + "  " + glyphs.Close
	for _, marker := range []string{actionCluster} {
		found := false
		for _, line := range frame.Lines {
			if strings.Contains(line, marker) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("v3 e2e smoke: pane action marker %q missing from frame %#v", marker, frame.Lines)
		}
	}
	for _, line := range frame.ANSILines {
		if strings.Contains(line, "\x1b[") {
			return nil
		}
	}
	return fmt.Errorf("v3 e2e smoke: styled chrome ANSI missing from frame %#v", frame.ANSILines)
}

func v3E2EFramesContain(frames []render.Frame, value string) bool {
	for _, frame := range frames {
		for _, line := range frame.Lines {
			if strings.Contains(line, value) {
				return true
			}
		}
	}
	return false
}

func drainV3RuntimeUntilFrameContains(ctx context.Context, runtime *app.AppRuntime, host *app.FakeTerminalHost, value string) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := runtime.Drain(deadlineCtx); err != nil {
			return err
		}
		if v3E2EFramesContain(host.Frames(), value) {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("v3 e2e smoke: timed out waiting for live backend update %q", value)
		case <-ticker.C:
		}
	}
}
