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

func rowToString(row []Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return b.String()
}
