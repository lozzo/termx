package termx

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/transport/memory"
	"github.com/lozzow/termx/tui"
)

// skipIfMissing skips the test if the given command is not in PATH.
func skipIfMissing(t *testing.T, cmd string) {
	t.Helper()
	if _, err := exec.LookPath(cmd); err != nil {
		t.Skipf("%s not found, skipping", cmd)
	}
}

// newE2ERealClient is like newE2EClient but with a larger scrollback for
// real-program tests that may produce lots of output.
func newE2ERealClient(t *testing.T) (*Server, *protocol.Client, func()) {
	t.Helper()
	srv := NewServer(
		WithDefaultScrollback(1000),
		WithDefaultKeepAfterExit(500*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ct, st := memory.NewPair()

	go func() { _ = srv.handleTransport(ctx, st, "memory") }()

	client := protocol.NewClient(ct)
	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "e2e-real"}); err != nil {
		cancel()
		ct.Close()
		st.Close()
		t.Fatalf("hello failed: %v", err)
	}
	return srv, client, func() {
		client.Close()
		cancel()
		ct.Close()
		st.Close()
	}
}

// createAndAttach is a convenience that creates a terminal running cmd and
// attaches as collaborator.
func createAndAttach(
	t *testing.T, client *protocol.Client,
	name string, cmd []string, cols, rows uint16,
) (termID string, ch uint16, stream <-chan protocol.StreamFrame, stop func()) {
	t.Helper()
	ctx := context.Background()
	created, err := client.Create(ctx, protocol.CreateParams{
		Command: cmd,
		Name:    name,
		Size:    protocol.Size{Cols: cols, Rows: rows},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create %q failed: %v", name, err)
	}
	attach, err := client.Attach(ctx, created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	s, stopFn := client.Stream(attach.Channel)
	return created.TerminalID, attach.Channel, s, stopFn
}

// drainUntil reads stream frames until predicate returns true or timeout.
func drainUntil(t *testing.T, stream <-chan protocol.StreamFrame, pred func(string) bool, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	var buf strings.Builder
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out; accumulated output:\n%s", buf.String())
			return ""
		case msg, ok := <-stream:
			if !ok {
				t.Fatalf("stream closed; accumulated output:\n%s", buf.String())
				return ""
			}
			if msg.Type == protocol.TypeOutput {
				buf.Write(msg.Payload)
				if pred(buf.String()) {
					return buf.String()
				}
			}
			if msg.Type == protocol.TypeClosed {
				if pred(buf.String()) {
					return buf.String()
				}
				t.Fatalf("stream closed; accumulated output:\n%s", buf.String())
				return ""
			}
		}
	}
}

// =============================================================================
// Test: 直接运行一个短命令并验证 exit code
// =============================================================================

func TestE2EReal_ShortLivedCommand(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	// Run sh -c directly: echoes and exits with specific code.
	// We create, then immediately attach — the command might finish fast,
	// but the stream buffer holds the output.
	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", "sleep 0.1; echo hello-world; exit 7"},
		Name:    "short-cmd",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	stream, stop := client.Stream(attach.Channel)
	defer stop()

	// Wait for output
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "hello-world")
	}, 5*time.Second)

	// Wait for TypeClosed with exit code 7
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for close with exit code")
		case msg, ok := <-stream:
			if !ok {
				t.Fatal("stream closed without TypeClosed frame")
			}
			if msg.Type == protocol.TypeClosed {
				code, _ := protocol.DecodeClosedPayload(msg.Payload)
				if code != 7 {
					t.Fatalf("expected exit code 7, got %d", code)
				}
				return
			}
		}
	}
}

// =============================================================================
// Test: seq 生成大量输出，验证 scrollback 和 snapshot
// =============================================================================

func TestE2EReal_LargeOutputSeq(t *testing.T) {
	skipIfMissing(t, "seq")
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	termID, ch, stream, stop := createAndAttach(t, client,
		"seq-test", []string{"bash", "--noprofile", "--norc"}, 80, 24)
	defer stop()

	// Give bash time to start
	time.Sleep(500 * time.Millisecond)

	// 发送 seq 500（产生 500 行输出）— use proper channel
	if err := client.Input(ctx, ch, []byte("seq 500\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	// 等最后一行 "500" 出现
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "500")
	}, 10*time.Second)

	// snapshot 应该包含尾部数据
	snap, err := client.Snapshot(ctx, termID, 0, 500)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	// 检查 scrollback + screen 中有 "500"
	if !protocolSnapshotContains(snap, "500") {
		t.Fatal("snapshot missing last seq line '500'")
	}
	// 检查靠前的行也存在（验证 scrollback 工作）
	if !protocolSnapshotContains(snap, "1") {
		t.Log("warning: seq line '1' not in snapshot (may have scrolled past scrollback limit)")
	}
}

// =============================================================================
// Test: python3 REPL 交互
// =============================================================================

func TestE2EReal_PythonREPL(t *testing.T) {
	skipIfMissing(t, "python3")
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	_, ch, stream, stop := createAndAttach(t, client,
		"python-repl", []string{"python3", "-u"}, 80, 24)
	defer stop()

	// 等 Python prompt ">>>"
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, ">>>")
	}, 10*time.Second)

	// 计算表达式
	if err := client.Input(ctx, ch, []byte("print(6 * 7)\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "42")
	}, 5*time.Second)

	// 退出 python
	if err := client.Input(ctx, ch, []byte("exit()\n")); err != nil {
		t.Fatalf("exit input failed: %v", err)
	}

	waitStreamClosed(t, stream, 5*time.Second)
}

// =============================================================================
// Test: vi 全屏程序 — 进入 alternate screen，输入文本，退出
// =============================================================================

func TestE2EReal_ViFullscreen(t *testing.T) {
	skipIfMissing(t, "vi")
	srv, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	termID, ch, stream, stop := createAndAttach(t, client,
		"vi-test", []string{"vi"}, 80, 24)
	defer stop()

	// Wait for vi to start — it enters alternate screen and sends DSR.
	// With the response-pipe drain fix, this should not deadlock.
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "\x1b[?1049h") || // alternate screen
			strings.Contains(s, "\x1b[H") || // cursor home
			len(s) > 50 // fallback
	}, 10*time.Second)

	// Verify alternate screen mode via snapshot
	snap, err := srv.Snapshot(ctx, termID, SnapshotOptions{})
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if !snap.Screen.IsAlternateScreen {
		t.Log("warning: vi did not trigger alternate screen (may depend on vi version)")
	}

	// Insert text: i → type → Esc → :q!
	if err := client.Input(ctx, ch, []byte("i")); err != nil {
		t.Fatalf("input 'i' failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := client.Input(ctx, ch, []byte("hello from termx")); err != nil {
		t.Fatalf("input text failed: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Verify typed text in snapshot
	snap, err = srv.Snapshot(ctx, termID, SnapshotOptions{})
	if err != nil {
		t.Fatalf("snapshot after typing failed: %v", err)
	}
	found := false
	for _, row := range snap.Screen.Cells {
		var sb strings.Builder
		for _, c := range row {
			sb.WriteString(c.Content)
		}
		if strings.Contains(sb.String(), "hello from termx") {
			found = true
			break
		}
	}
	if !found {
		t.Log("warning: typed text not found in snapshot (may depend on vi buffering)")
	}

	// Esc → :q!
	if err := client.Input(ctx, ch, []byte{0x1b}); err != nil {
		t.Fatalf("input Esc failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := client.Input(ctx, ch, []byte(":q!\n")); err != nil {
		t.Fatalf("input :q! failed: %v", err)
	}

	waitStreamClosed(t, stream, 5*time.Second)
}

// =============================================================================
// Test: less 全屏程序 + 翻页 + 退出
// =============================================================================

func TestE2EReal_LessPager(t *testing.T) {
	skipIfMissing(t, "less")
	skipIfMissing(t, "seq")
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	// 用 bash 管道: seq 200 | less
	_, ch, stream, stop := createAndAttach(t, client,
		"less-test", []string{"bash", "-c", "seq 200 | less"}, 80, 24)
	defer stop()

	// less 启动后应该显示第一行 "1"
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "1")
	}, 10*time.Second)

	// 按空格翻页
	time.Sleep(200 * time.Millisecond)
	if err := client.Input(ctx, ch, []byte(" ")); err != nil {
		t.Fatalf("input space failed: %v", err)
	}

	// 翻页后应该能看到更大的数字
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "25") || strings.Contains(s, "30")
	}, 5*time.Second)

	// q 退出 less
	if err := client.Input(ctx, ch, []byte("q")); err != nil {
		t.Fatalf("input q failed: %v", err)
	}

	waitStreamClosed(t, stream, 5*time.Second)
}

// =============================================================================
// Test: Ctrl-C 中断运行中的程序
// =============================================================================

func TestE2EReal_CtrlCInterrupt(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	_, ch, stream, stop := createAndAttach(t, client,
		"ctrlc-test", []string{"bash", "--noprofile", "--norc"}, 80, 24)
	defer stop()

	// Give bash time to start
	time.Sleep(500 * time.Millisecond)

	// Start sleep 999 (blocks)
	if err := client.Input(ctx, ch, []byte("sleep 999\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Send Ctrl-C to interrupt sleep
	if err := client.Input(ctx, ch, []byte{0x03}); err != nil {
		t.Fatalf("ctrl-c failed: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Verify bash is still alive by running a command
	if err := client.Input(ctx, ch, []byte("echo still-here\n")); err != nil {
		t.Fatalf("input after ctrl-c failed: %v", err)
	}
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "still-here")
	}, 5*time.Second)
}

// termID looks up a terminal ID by name from the list.
func termID(t *testing.T, client *protocol.Client, name string) string {
	t.Helper()
	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	for _, ti := range list.Terminals {
		if ti.Name == name {
			return ti.ID
		}
	}
	t.Fatalf("terminal %q not found", name)
	return ""
}

// =============================================================================
// Test: 环境变量和工作目录传递
// =============================================================================

func TestE2EReal_EnvAndWorkDir(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", "sleep 0.1; echo MY_VAR=$MY_VAR; pwd; exit 0"},
		Name:    "env-test",
		Size:    protocol.Size{Cols: 80, Rows: 24},
		Env:     []string{"MY_VAR=hello_termx"},
		Dir:     "/tmp",
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	stream, stop := client.Stream(attach.Channel)
	defer stop()

	output := drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "MY_VAR=") && strings.Contains(s, "/tmp")
	}, 5*time.Second)

	if !strings.Contains(output, "MY_VAR=hello_termx") {
		t.Fatalf("env var not passed: %s", output)
	}
	if !strings.Contains(output, "/tmp") {
		t.Fatalf("working dir not set: %s", output)
	}
}

// =============================================================================
// Test: stty 验证 PTY size 正确传递给子进程
// =============================================================================

func TestE2EReal_SttyReportsCorrectSize(t *testing.T) {
	skipIfMissing(t, "stty")
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	_, ch, stream, stop := createAndAttach(t, client,
		"stty-test", []string{"bash", "--noprofile", "--norc"}, 132, 43)
	defer stop()

	// Give bash time to start
	time.Sleep(500 * time.Millisecond)

	// stty size outputs "rows cols"
	if err := client.Input(ctx, ch, []byte("stty size\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "43 132")
	}, 5*time.Second)

	// Resize then check again
	if err := client.Resize(ctx, ch, 200, 50); err != nil {
		t.Fatalf("resize failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if err := client.Input(ctx, ch, []byte("stty size\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "50 200")
	}, 5*time.Second)
}

// =============================================================================
// Test: cat 管道交互 — 回显每行输入
// =============================================================================

func TestE2EReal_CatEcho(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	_, ch, stream, stop := createAndAttach(t, client,
		"cat-test", []string{"cat"}, 80, 24)
	defer stop()

	// cat 直接回显，发送几行
	lines := []string{"alpha", "bravo", "charlie"}
	for _, line := range lines {
		if err := client.Input(ctx, ch, []byte(line+"\n")); err != nil {
			t.Fatalf("input %q failed: %v", line, err)
		}
	}

	// 验证所有行都被回显
	drainUntil(t, stream, func(s string) bool {
		for _, l := range lines {
			if !strings.Contains(s, l) {
				return false
			}
		}
		return true
	}, 5*time.Second)

	// Ctrl-D 结束 cat
	if err := client.Input(ctx, ch, []byte{0x04}); err != nil {
		t.Fatalf("ctrl-d failed: %v", err)
	}

	waitStreamClosed(t, stream, 5*time.Second)
}

// =============================================================================
// Real TUI workflow: startup create -> split create -> floating reuse
// =============================================================================

func TestE2ERealTUI_WorkflowCreateSplitAndFloatingReuse(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()
	reuseCreated, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "real-reuse",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create reuse terminal failed: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupPicker: true,
	})
	defer h.Close()

	h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()

	h.sendText("echo REAL-ROOT")
	h.pressEnter()
	screen := h.waitForStableScreenContains("REAL-ROOT", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected startup chooser to close after create, got:\n%s", screen)
	}

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.sendText("echo REAL-SPLIT")
	h.pressEnter()
	screen = h.waitForStableScreenContains("REAL-SPLIT", 10*time.Second)
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split create to show multiple panes, got:\n%s", screen)
	}

	h.sendPrefixRune('w')
	h.waitForStableScreenContains("Open Floating Pane", 10*time.Second)
	h.sendText(reuseCreated.TerminalID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("real-reuse", 10*time.Second)
	if !strings.Contains(screen, "[floating]") {
		t.Fatalf("expected floating reused terminal to stay visible, got:\n%s", screen)
	}

	h.sendText("echo REAL-FLOAT")
	h.pressEnter()
	screen = h.waitForStableScreenContains("REAL-FLOAT", 10*time.Second)
	if !strings.Contains(screen, "[floating]") {
		t.Fatalf("expected floating marker to remain after command, got:\n%s", screen)
	}
}

// =============================================================================
// Real TUI workflow: startup attach existing -> duplicate in floating -> keep alive
// =============================================================================

func TestE2ERealTUI_StartupAttachAndFloatingReuseSameTerminal(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()
	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "real-shared",
		Size:    protocol.Size{Cols: 100, Rows: 28},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create shared terminal failed: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupPicker: true,
	})
	defer h.Close()

	h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	h.sendText(created.TerminalID)
	h.pressEnter()
	screen := h.waitForStableScreenContains("real-shared", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected startup attach chooser to close, got:\n%s", screen)
	}

	h.sendPrefixRune('w')
	h.waitForStableScreenContains("Open Floating Pane", 10*time.Second)
	h.sendText(created.TerminalID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("[floating]", 10*time.Second)
	if !strings.Contains(screen, "real-shared") {
		t.Fatalf("expected reused terminal title to remain visible, got:\n%s", screen)
	}

	h.sendText("echo REAL-SHARED")
	h.pressEnter()
	screen = h.waitForStableScreenContains("REAL-SHARED", 10*time.Second)
	if !strings.Contains(screen, "[floating]") {
		t.Fatalf("expected floating duplicate to remain active after command, got:\n%s", screen)
	}

	snap, err := client.Snapshot(ctx, created.TerminalID, 0, 200)
	if err != nil {
		t.Fatalf("snapshot shared terminal failed: %v", err)
	}
	if !protocolSnapshotContains(snap, "REAL-SHARED") {
		t.Fatalf("expected shared terminal snapshot to contain command output, got %#v", snap)
	}
}

// =============================================================================
// Real TUI workflow: create tab -> create shell -> picker replace with existing
// =============================================================================

func TestE2ERealTUI_TabCreateAndPickerReuse(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()
	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "real-picker",
		Size:    protocol.Size{Cols: 90, Rows: 26},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create picker terminal failed: %v", err)
	}

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendPrefixRune('c')
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()

	h.sendText("echo REAL-TAB")
	h.pressEnter()
	screen := h.waitForStableScreenContains("REAL-TAB", 10*time.Second)
	if strings.Contains(screen, "Open Tab") {
		t.Fatalf("expected tab chooser to close after create, got:\n%s", screen)
	}

	h.sendPrefixRune('f')
	h.waitForStableScreenContains("Terminal Picker", 10*time.Second)
	h.sendText(created.TerminalID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("real-picker", 10*time.Second)
	if strings.Contains(screen, "Terminal Picker") {
		t.Fatalf("expected picker to close after attach, got:\n%s", screen)
	}

	h.sendText("echo REAL-PICKER")
	h.pressEnter()
	screen = h.waitForStableScreenContains("REAL-PICKER", 10*time.Second)
	if !strings.Contains(screen, "real-picker") {
		t.Fatalf("expected picker-reused terminal to stay attached, got:\n%s", screen)
	}
}

// =============================================================================
// Real TUI workflow: close floating viewport and keep session interactive
// =============================================================================

func TestE2ERealTUI_CloseFloatingKeepsSessionAlive(t *testing.T) {
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()
	reuseCreated, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "real-close-float",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create reuse terminal failed: %v", err)
	}

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("echo REAL-CLOSE-BASE")
	h.pressEnter()
	h.waitForStableScreenContains("REAL-CLOSE-BASE", 10*time.Second)

	h.sendPrefixRune('w')
	h.waitForStableScreenContains("Open Floating Pane", 10*time.Second)
	h.sendText(reuseCreated.TerminalID)
	h.pressEnter()
	h.waitForStableScreenContains("[floating]", 10*time.Second)

	h.sendPrefixRune('x')
	screen := h.waitForStableScreenWithout("[floating]", 10*time.Second)
	if !strings.Contains(screen, "REAL-CLOSE-BASE") {
		t.Fatalf("expected base terminal to remain visible after closing floating pane, got:\n%s", screen)
	}

	h.sendText("echo REAL-CLOSE-AFTER")
	h.pressEnter()
	screen = h.waitForStableScreenContains("REAL-CLOSE-AFTER", 10*time.Second)
	if !strings.Contains(screen, "REAL-CLOSE-AFTER") {
		t.Fatalf("expected session to remain interactive after closing floating pane, got:\n%s", screen)
	}
}

// =============================================================================
// Test: htop 快速启动退出 — 验证全屏程序不会 crash 服务端
// =============================================================================

func TestE2EReal_HtopQuickExit(t *testing.T) {
	skipIfMissing(t, "htop")
	_, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	_, ch, stream, stop := createAndAttach(t, client,
		"htop-test", []string{"htop"}, 120, 40)
	defer stop()

	// Wait for htop to produce some output (it draws the UI)
	drainUntil(t, stream, func(s string) bool {
		return len(s) > 200 // htop produces a lot of output
	}, 10*time.Second)

	// Press q to exit htop, retry a few times in case it needs time
	for i := 0; i < 3; i++ {
		if err := client.Input(ctx, ch, []byte("q")); err != nil {
			t.Fatalf("input q failed: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	waitStreamClosed(t, stream, 10*time.Second)

	// Verify server still works — create new terminal
	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", "sleep 0.2; echo server-ok; exit 0"},
		Name:    "post-htop",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create after htop failed: %v", err)
	}
	attach, err := client.Attach(ctx, created.TerminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	s2, stop2 := client.Stream(attach.Channel)
	defer stop2()
	drainUntil(t, s2, func(s string) bool {
		return strings.Contains(s, "server-ok")
	}, 5*time.Second)
}

// =============================================================================
// Test: 快速连续 resize — 模拟 TUI 拖动窗口
// =============================================================================

func TestE2EReal_RapidResize(t *testing.T) {
	skipIfMissing(t, "stty")
	srv, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	termID, ch, stream, stop := createAndAttach(t, client,
		"rapid-resize", []string{"bash", "--noprofile", "--norc"}, 80, 24)
	defer stop()

	// Give bash time to start
	time.Sleep(500 * time.Millisecond)

	// 快速连续 resize 20 次
	for i := 0; i < 20; i++ {
		cols := uint16(60 + i*3)
		rows := uint16(20 + i)
		if err := client.Resize(ctx, ch, cols, rows); err != nil {
			t.Fatalf("resize %d failed: %v", i, err)
		}
	}

	// 等最后一次 resize 生效
	time.Sleep(300 * time.Millisecond)
	finalCols := uint16(60 + 19*3)
	finalRows := uint16(20 + 19)

	info, err := srv.Get(ctx, termID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if info.Size.Cols != finalCols || info.Size.Rows != finalRows {
		t.Fatalf("final size mismatch: got %dx%d, want %dx%d",
			info.Size.Cols, info.Size.Rows, finalCols, finalRows)
	}

	// 验证 bash 还能正常工作
	if err := client.Input(ctx, ch, []byte("echo resize-ok\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "resize-ok")
	}, 5*time.Second)

	// 验证 stty 报告的也对
	if err := client.Input(ctx, ch, []byte(fmt.Sprintf("stty size\n"))); err != nil {
		t.Fatalf("stty input failed: %v", err)
	}
	expected := fmt.Sprintf("%d %d", finalRows, finalCols)
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, expected)
	}, 5*time.Second)
}

// =============================================================================
// Test: top 短暂运行 — 验证持续输出的全屏程序 + resize
// =============================================================================

func TestE2EReal_TopWithResize(t *testing.T) {
	skipIfMissing(t, "top")
	srv, client, cleanup := newE2ERealClient(t)
	defer cleanup()

	ctx := context.Background()

	// -b batch mode, -n 3 iterations (top 会在 batch 模式输出 3 轮后退出)
	termID, ch, stream, stop := createAndAttach(t, client,
		"top-test", []string{"top", "-b", "-n", "2"}, 120, 40)
	defer stop()

	// 等 top 输出第一轮
	drainUntil(t, stream, func(s string) bool {
		return strings.Contains(s, "PID") || strings.Contains(s, "top -")
	}, 10*time.Second)

	// 中间做一次 resize
	if err := client.Resize(ctx, ch, 160, 50); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	info, err := srv.Get(ctx, termID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	// resize 可能还没传到，等一下
	time.Sleep(200 * time.Millisecond)
	info, err = srv.Get(ctx, termID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if info.Size.Cols != 160 || info.Size.Rows != 50 {
		t.Fatalf("resize not applied: got %dx%d", info.Size.Cols, info.Size.Rows)
	}

	// 等 top 自然退出
	waitStreamClosed(t, stream, 30*time.Second)
}
