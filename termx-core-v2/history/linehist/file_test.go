package linehist

import (
	"os"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

// LineFile 是 append-only 二进制文件：offset 索引 + 分页读 + 崩溃截尾恢复。

func styledLineForTest() Line {
	return Line{
		Runs: []Run{
			{Text: "plain "},
			{Text: "red", Style: history.CellStyle{FG: "1", Bold: true}},
			{Text: "link", LinkURL: "https://example.com", LinkParams: "id=9"},
		},
		HardEnd: true,
	}
}

func TestLineFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file, err := OpenLineFile(dir, "term-1")
	if err != nil {
		t.Fatalf("open line file: %v", err)
	}
	defer file.Close()
	want := []Line{
		styledLineForTest(),
		{Runs: nil, HardEnd: true},                      // 空行
		{Runs: []Run{{Text: "chunk"}}, HardEnd: false},  // chunk 记录
		{Runs: []Run{{Text: "你好世界"}}, HardEnd: true}, // 宽字符
	}
	if err := file.AppendLines(want); err != nil {
		t.Fatalf("append lines: %v", err)
	}
	if file.LineCount() != len(want) {
		t.Fatalf("line count = %d, want %d", file.LineCount(), len(want))
	}
	got, err := file.Lines(0, file.LineCount())
	if err != nil {
		t.Fatalf("read lines: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if LineText(got[i]) != LineText(want[i]) || got[i].HardEnd != want[i].HardEnd {
			t.Fatalf("line %d = %#v, want %#v", i, got[i], want[i])
		}
	}
	run := got[0].Runs[1]
	if run.Style.FG != "1" || !run.Style.Bold {
		t.Fatalf("style must round-trip: %#v", run)
	}
	link := got[0].Runs[2]
	if link.LinkURL != "https://example.com" || link.LinkParams != "id=9" {
		t.Fatalf("link must round-trip: %#v", link)
	}
}

func TestLineFileReopenRecoversOffsets(t *testing.T) {
	dir := t.TempDir()
	file, err := OpenLineFile(dir, "term-1")
	if err != nil {
		t.Fatalf("open line file: %v", err)
	}
	lines := []Line{
		{Runs: []Run{{Text: "one"}}, HardEnd: true},
		{Runs: []Run{{Text: "two"}}, HardEnd: true},
		{Runs: []Run{{Text: "three"}}, HardEnd: true},
	}
	if err := file.AppendLines(lines); err != nil {
		t.Fatalf("append lines: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := OpenLineFile(dir, "term-1")
	if err != nil {
		t.Fatalf("reopen line file: %v", err)
	}
	defer reopened.Close()
	if reopened.LineCount() != 3 {
		t.Fatalf("recovered count = %d, want 3", reopened.LineCount())
	}
	got, err := reopened.Lines(0, 3)
	if err != nil {
		t.Fatalf("read after recover: %v", err)
	}
	texts := lineTextsForTest(got)
	if strings.Join(texts, "|") != "one|two|three" {
		t.Fatalf("recovered lines = %v", texts)
	}
	// 恢复后必须还能继续追加。
	if err := reopened.AppendLines([]Line{{Runs: []Run{{Text: "four"}}, HardEnd: true}}); err != nil {
		t.Fatalf("append after recover: %v", err)
	}
	got, err = reopened.Lines(3, 4)
	if err != nil || len(got) != 1 || LineText(got[0]) != "four" {
		t.Fatalf("appended line after recover = %v err=%v", lineTextsForTest(got), err)
	}
}

func TestLineFilePagedReads(t *testing.T) {
	dir := t.TempDir()
	file, err := OpenLineFile(dir, "term-1")
	if err != nil {
		t.Fatalf("open line file: %v", err)
	}
	defer file.Close()
	var lines []Line
	for _, text := range []string{"l0", "l1", "l2", "l3", "l4"} {
		lines = append(lines, Line{Runs: []Run{{Text: text}}, HardEnd: true})
	}
	if err := file.AppendLines(lines); err != nil {
		t.Fatalf("append lines: %v", err)
	}
	mid, err := file.Lines(1, 4)
	if err != nil {
		t.Fatalf("paged read: %v", err)
	}
	if strings.Join(lineTextsForTest(mid), "|") != "l1|l2|l3" {
		t.Fatalf("paged lines = %v, want l1|l2|l3", lineTextsForTest(mid))
	}
	// 越界收敛，不报错。
	clamped, err := file.Lines(-3, 99)
	if err != nil || len(clamped) != 5 {
		t.Fatalf("clamped read = %d lines err=%v, want 5", len(clamped), err)
	}
	empty, err := file.Lines(4, 2)
	if err != nil || len(empty) != 0 {
		t.Fatalf("inverted range must be empty, got %d err=%v", len(empty), err)
	}
}

func TestLineFileTruncatesPartialTailRecord(t *testing.T) {
	dir := t.TempDir()
	file, err := OpenLineFile(dir, "term-1")
	if err != nil {
		t.Fatalf("open line file: %v", err)
	}
	if err := file.AppendLines([]Line{
		{Runs: []Run{{Text: "keep1"}}, HardEnd: true},
		{Runs: []Run{{Text: "keep2"}}, HardEnd: true},
	}); err != nil {
		t.Fatalf("append lines: %v", err)
	}
	path := file.Path()
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// 模拟崩溃：往文件尾写半截 header（合法魔数 + 超出文件的 payload 长度）。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	partial := []byte{0x4C, 0x4C, 0x58, 0x54, 0x01, 0x00, 0x01, 0x01, 0x00, 0x00}
	if err := os.WriteFile(path, append(raw, partial...), 0o600); err != nil {
		t.Fatalf("write partial tail: %v", err)
	}

	reopened, err := OpenLineFile(dir, "term-1")
	if err != nil {
		t.Fatalf("reopen with partial tail: %v", err)
	}
	defer reopened.Close()
	if reopened.LineCount() != 2 {
		t.Fatalf("recovered count = %d, want 2 (partial tail truncated)", reopened.LineCount())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat truncated file: %v", err)
	}
	if info.Size() != int64(len(raw)) {
		t.Fatalf("file size = %d, want %d (partial tail removed)", info.Size(), len(raw))
	}
	if err := reopened.AppendLines([]Line{{Runs: []Run{{Text: "after"}}, HardEnd: true}}); err != nil {
		t.Fatalf("append after truncate: %v", err)
	}
	got, err := reopened.Lines(0, 3)
	if err != nil || strings.Join(lineTextsForTest(got), "|") != "keep1|keep2|after" {
		t.Fatalf("lines after truncate recover = %v err=%v", lineTextsForTest(got), err)
	}
}
