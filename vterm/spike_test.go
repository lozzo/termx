package vterm

import (
	charmvt "github.com/charmbracelet/x/vt"
	"strings"
	"sync"
	"testing"
)

func TestVTermBasicBehavior(t *testing.T) {
	vt := New(5, 2, 2, nil)

	if _, err := vt.Write([]byte("\x1b[31mA\x1b[0m")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cell := vt.CellAt(0, 0)
	if cell.Content != "A" {
		t.Fatalf("unexpected content: %#v", cell)
	}
	if cell.Style.FG == "" {
		t.Fatal("expected foreground color")
	}

	if _, err := vt.Write([]byte("1\n2\n3\n4\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	scrollback := vt.ScrollbackContent()
	if len(scrollback) == 0 {
		t.Fatal("expected scrollback")
	}
	found := false
	for _, row := range scrollback {
		if strings.TrimSpace(rowToString(row)) != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected scrollback to contain content")
	}

	if _, err := vt.Write([]byte("\x1b[?1049h")); err != nil {
		t.Fatalf("alt screen write failed: %v", err)
	}
	if !vt.IsAltScreen() {
		t.Fatal("expected alt screen")
	}
}

func TestVTermConcurrentAccess(t *testing.T) {
	vt := New(80, 24, 10, nil)
	var wg sync.WaitGroup

	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = vt.Write([]byte("hello"))
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = vt.CellAt(0, 0)
			}
		}()
	}

	wg.Wait()
}

func TestVTermTracksApplicationCursorMode(t *testing.T) {
	vt := New(80, 24, 10, nil)

	if _, err := vt.Write([]byte("\x1b[?1h")); err != nil {
		t.Fatalf("enable application cursor failed: %v", err)
	}
	if !vt.Modes().ApplicationCursor {
		t.Fatal("expected application cursor mode to be enabled")
	}

	if _, err := vt.Write([]byte("\x1b[?1l")); err != nil {
		t.Fatalf("disable application cursor failed: %v", err)
	}
	if vt.Modes().ApplicationCursor {
		t.Fatal("expected application cursor mode to be disabled")
	}
}

func TestVTermPreservesAnsiIndexedColorSemantic(t *testing.T) {
	vt := New(10, 3, 10, nil)
	vt.SetIndexedColor(1, "#123456")

	if _, err := vt.Write([]byte("\x1b[31mR\x1b[0m")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cell := vt.CellAt(0, 0)
	if cell.Style.FG != "ansi:1" {
		t.Fatalf("expected ANSI semantic color token, got %#v", cell.Style)
	}
}

func TestVTermNormalizesPlainUTF8CombiningText(t *testing.T) {
	vt := New(20, 5, 10, nil)

	if _, err := vt.Write([]byte("e\u0301🙂한글")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	row := vt.ScreenContent().Cells[0]
	if got := rowToString(row); !strings.Contains(got, "é🙂한글") {
		t.Fatalf("expected normalized text in row, got %q", got)
	}
}

func TestVTermWriteRecoversFromEmulatorPanic(t *testing.T) {
	vt := New(20, 5, 10, nil)

	prev := safeEmulatorWrite
	safeEmulatorWrite = func(_ *charmvt.SafeEmulator, _ []byte) (int, error) {
		panic("boom")
	}
	t.Cleanup(func() {
		safeEmulatorWrite = prev
	})

	if _, err := vt.Write([]byte("hello")); err == nil {
		t.Fatal("expected write to convert emulator panic into error")
	} else if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected panic context in error, got %v", err)
	}
}

func TestVTermPreservesRowTimestampAcrossScroll(t *testing.T) {
	vt := New(4, 2, 10, nil)

	if _, err := vt.Write([]byte("abcd\r\nefgh")); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	firstRowTS := vt.ScreenRowTimestampAt(0)
	if firstRowTS.IsZero() {
		t.Fatal("expected first row timestamp to be set")
	}

	if _, err := vt.Write([]byte("\r\nijkl")); err != nil {
		t.Fatalf("scroll write failed: %v", err)
	}

	if got := strings.TrimSpace(rowToString(vt.ScrollbackRow(0))); got != "abcd" {
		t.Fatalf("expected first row to scroll into scrollback, got %q", got)
	}
	if got := vt.ScrollbackRowTimestampAt(0); !got.Equal(firstRowTS) {
		t.Fatalf("expected scrollback timestamp %v, got %v", firstRowTS, got)
	}
}

func TestVTermRowViewsReuseCacheWithoutExposingMutableRows(t *testing.T) {
	vt := New(4, 2, 10, nil)

	if _, err := vt.Write([]byte("abcd\r\nefgh\r\nijkl")); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	screenViewA := vt.ScreenRowView(1)
	screenViewB := vt.ScreenRowView(1)
	if len(screenViewA) == 0 || len(screenViewB) == 0 {
		t.Fatal("expected cached screen row view")
	}
	if &screenViewA[0] != &screenViewB[0] {
		t.Fatal("expected screen row view to reuse cached backing storage")
	}

	screenCopy := vt.ScreenRow(1)
	screenCopy[0].Content = "z"
	if got := strings.TrimSpace(rowToString(vt.ScreenRow(1))); got != "ijkl" {
		t.Fatalf("expected ScreenRow to return a copy, got %q", got)
	}
	if got := strings.TrimSpace(rowToString(vt.ScreenRowView(1))); got != "ijkl" {
		t.Fatalf("expected ScreenRowView to remain unchanged, got %q", got)
	}

	scrollViewA := vt.ScrollbackRowView(0)
	scrollViewB := vt.ScrollbackRowView(0)
	if len(scrollViewA) == 0 || len(scrollViewB) == 0 {
		t.Fatal("expected cached scrollback row view")
	}
	if &scrollViewA[0] != &scrollViewB[0] {
		t.Fatal("expected scrollback row view to reuse cached backing storage")
	}

	scrollCopy := vt.ScrollbackRow(0)
	scrollCopy[0].Content = "z"
	if got := strings.TrimSpace(rowToString(vt.ScrollbackRow(0))); got != "abcd" {
		t.Fatalf("expected ScrollbackRow to return a copy, got %q", got)
	}
	if got := strings.TrimSpace(rowToString(vt.ScrollbackRowView(0))); got != "abcd" {
		t.Fatalf("expected ScrollbackRowView to remain unchanged, got %q", got)
	}
}

func TestVTermConservativeScreenRowsMatchDefaultExtraction(t *testing.T) {
	vt := New(10, 3, 10, nil)
	vt.SetIndexedColor(1, "#123456")

	if _, err := vt.Write([]byte("\x1b[31mR\x1b[0m你\x1b[48;2;17;34;51m \x1b[0mZ\r\n\x1b[4ma\u0301\x1b[0m🙂")); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	want := vt.ScreenContent()
	t.Setenv("TERMX_VTERM_CONSERVATIVE_SCREEN_ROWS", "1")
	got := vt.ScreenContent()
	assertScreenDataEqual(t, got, want)

	state := vt.SnapshotRenderState()
	assertScreenDataEqual(t, state.Screen, want)
}

func TestVTermConservativeScreenRowsPreserveWideCellContinuations(t *testing.T) {
	t.Setenv("TERMX_VTERM_CONSERVATIVE_SCREEN_ROWS", "1")

	vt := New(8, 2, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "你", Width: 2},
				{Content: "", Width: 0},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
			},
		},
	}, CursorState{Row: 0, Col: 5, Visible: true}, TerminalModes{AutoWrap: true})

	screen := vt.ScreenContent()
	if got := screen.Cells[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected first wide cell restored, got %#v", got)
	}
	if got := screen.Cells[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder at x=1, got %#v", got)
	}
	if got := screen.Cells[0][2]; got.Content != "好" || got.Width != 2 {
		t.Fatalf("expected second wide cell restored, got %#v", got)
	}
	if got := screen.Cells[0][3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder at x=3, got %#v", got)
	}

	if _, err := vt.Write([]byte("!")); err != nil {
		t.Fatalf("write after snapshot failed: %v", err)
	}

	screen = vt.ScreenContent()
	if got := screen.Cells[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder at x=1 after write, got %#v", got)
	}
	if got := screen.Cells[0][3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder at x=3 after write, got %#v", got)
	}
	if got := screen.Cells[0][5]; got.Content != "!" || got.Width != 1 {
		t.Fatalf("expected trailing ASCII write after wide cells, got %#v", got)
	}
}

func rowToString(row []Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return b.String()
}

func assertScreenDataEqual(t *testing.T, got, want ScreenData) {
	t.Helper()
	if got.IsAlternateScreen != want.IsAlternateScreen {
		t.Fatalf("alternate-screen mismatch: got=%v want=%v", got.IsAlternateScreen, want.IsAlternateScreen)
	}
	if len(got.Cells) != len(want.Cells) {
		t.Fatalf("screen height mismatch: got=%d want=%d", len(got.Cells), len(want.Cells))
	}
	for y := range want.Cells {
		if len(got.Cells[y]) != len(want.Cells[y]) {
			t.Fatalf("screen width mismatch row=%d got=%d want=%d", y, len(got.Cells[y]), len(want.Cells[y]))
		}
		for x := range want.Cells[y] {
			if got.Cells[y][x] != want.Cells[y][x] {
				t.Fatalf("screen diverged at (%d,%d): got=%#v want=%#v", x, y, got.Cells[y][x], want.Cells[y][x])
			}
		}
	}
}
