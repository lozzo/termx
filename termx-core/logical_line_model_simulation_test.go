package termx

import (
	"reflect"
	"strings"
	"testing"
)

// logicalLineModel is a test-only simulator for the proposed future truth
// model: sealed logical lines in history plus one mutable open logical line.
// It intentionally models only single-width cells so we can validate the shape
// of the semantics before rewriting the production storage/runtime layers.
type logicalLineModel struct {
	cols        int
	rows        int
	sealed      [][]rune
	open        []rune
	cursor      int
	wrapPending bool
}

func newLogicalLineModel(cols, rows int) *logicalLineModel {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	return &logicalLineModel{cols: cols, rows: rows}
}

func (m *logicalLineModel) write(text string) {
	for _, r := range text {
		m.writeRune(r)
	}
}

func (m *logicalLineModel) writeRune(r rune) {
	if m == nil {
		return
	}
	if m.wrapPending {
		m.wrapPending = false
	}
	m.ensureOpenLen(m.cursor + 1)
	m.open[m.cursor] = r
	m.cursor++
	if m.cols > 0 && m.cursor > 0 && m.cursor%m.cols == 0 {
		m.wrapPending = true
	}
}

func (m *logicalLineModel) lineFeed() {
	if m == nil {
		return
	}
	m.sealed = append(m.sealed, append([]rune(nil), m.open...))
	m.open = nil
	m.cursor = 0
	m.wrapPending = false
}

func (m *logicalLineModel) carriageReturn() {
	if m == nil {
		return
	}
	m.cursor = m.currentVisualRowStart()
	m.wrapPending = false
}

func (m *logicalLineModel) moveCursorCol(col int) {
	if m == nil {
		return
	}
	if col < 0 {
		col = 0
	}
	if col >= m.cols {
		col = m.cols - 1
	}
	m.cursor = m.currentVisualRowStart() + col
	m.ensureOpenLen(m.cursor)
	m.wrapPending = false
}

func (m *logicalLineModel) eraseToEOL() {
	if m == nil {
		return
	}
	rowStart := m.currentVisualRowStart()
	rowEnd := rowStart + m.cols
	m.ensureOpenLen(rowEnd)
	for i := m.cursor; i < rowEnd; i++ {
		m.open[i] = ' '
	}
}

func (m *logicalLineModel) eraseChars(count int) {
	if m == nil || count <= 0 {
		return
	}
	rowStart := m.currentVisualRowStart()
	rowEnd := rowStart + m.cols
	m.ensureOpenLen(rowEnd)
	for i := m.cursor; i < rowEnd && i < m.cursor+count; i++ {
		m.open[i] = ' '
	}
}

func (m *logicalLineModel) insertChars(count int) {
	if m == nil || count <= 0 {
		return
	}
	rowStart := m.currentVisualRowStart()
	rowEnd := rowStart + m.cols
	m.ensureOpenLen(rowEnd)
	col := m.cursor - rowStart
	row := append([]rune(nil), m.open[rowStart:rowEnd]...)
	for i := len(row) - 1; i >= col+count; i-- {
		row[i] = row[i-count]
	}
	for i := col; i < col+count && i < len(row); i++ {
		row[i] = ' '
	}
	copy(m.open[rowStart:rowEnd], row)
}

func (m *logicalLineModel) deleteChars(count int) {
	if m == nil || count <= 0 {
		return
	}
	rowStart := m.currentVisualRowStart()
	rowEnd := rowStart + m.cols
	m.ensureOpenLen(rowEnd)
	col := m.cursor - rowStart
	row := append([]rune(nil), m.open[rowStart:rowEnd]...)
	for i := col; i < len(row); i++ {
		src := i + count
		if src < len(row) {
			row[i] = row[src]
		} else {
			row[i] = ' '
		}
	}
	copy(m.open[rowStart:rowEnd], row)
}

func (m *logicalLineModel) resize(cols, rows int) {
	if m == nil {
		return
	}
	if cols > 0 {
		m.cols = cols
	}
	if rows > 0 {
		m.rows = rows
	}
}

func (m *logicalLineModel) sealedTexts() []string {
	out := make([]string, 0, len(m.sealed))
	for _, line := range m.sealed {
		out = append(out, string(line))
	}
	return out
}

func (m *logicalLineModel) openText() string {
	if m == nil {
		return ""
	}
	return string(m.open)
}

func (m *logicalLineModel) projectAllRows() []string {
	if m == nil {
		return nil
	}
	var out []string
	for _, line := range m.sealed {
		out = append(out, projectLogicalLine(line, m.cols)...)
	}
	if len(m.open) > 0 {
		out = append(out, projectLogicalLine(m.open, m.cols)...)
	}
	return out
}

func (m *logicalLineModel) visibleRows() []string {
	rows := m.projectAllRows()
	if len(rows) <= m.rows {
		return rows
	}
	return rows[len(rows)-m.rows:]
}

func (m *logicalLineModel) currentVisualRowStart() int {
	if m == nil || m.cols <= 0 {
		return 0
	}
	if m.wrapPending && m.cursor > 0 {
		return ((m.cursor - 1) / m.cols) * m.cols
	}
	return (m.cursor / m.cols) * m.cols
}

func (m *logicalLineModel) ensureOpenLen(length int) {
	if m == nil || length <= len(m.open) {
		return
	}
	m.open = append(m.open, make([]rune, length-len(m.open))...)
	for i := range m.open {
		if m.open[i] == 0 {
			m.open[i] = ' '
		}
	}
}

func projectLogicalLine(line []rune, cols int) []string {
	if cols <= 0 {
		cols = 1
	}
	if len(line) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(line)+cols-1)/cols)
	for start := 0; start < len(line); start += cols {
		end := start + cols
		if end > len(line) {
			end = len(line)
		}
		out = append(out, string(line[start:end]))
	}
	return out
}

func TestLogicalLineModelIncrementalOutputStaysOpen(t *testing.T) {
	m := newLogicalLineModel(5, 3)
	m.write("ab")
	m.write("cd")

	if got := m.sealedTexts(); len(got) != 0 {
		t.Fatalf("expected no sealed lines, got %#v", got)
	}
	if got := m.openText(); got != "abcd" {
		t.Fatalf("expected one open logical line, got %q", got)
	}
	if got := m.projectAllRows(); !reflect.DeepEqual(got, []string{"abcd"}) {
		t.Fatalf("expected one projected row, got %#v", got)
	}
}

func TestLogicalLineModelSoftWrapKeepsOneTruthLine(t *testing.T) {
	m := newLogicalLineModel(5, 3)
	m.write("abcdefghij")

	if got := m.openText(); got != "abcdefghij" {
		t.Fatalf("expected one logical line truth, got %q", got)
	}
	if got := m.projectAllRows(); !reflect.DeepEqual(got, []string{"abcde", "fghij"}) {
		t.Fatalf("expected soft-wrap projection, got %#v", got)
	}
	if got := strings.Join(m.projectAllRows(), ""); got != "abcdefghij" {
		t.Fatalf("expected copy-like join to preserve one logical line, got %q", got)
	}
}

func TestLogicalLineModelExactWidthWithoutAndWithHardNewline(t *testing.T) {
	open := newLogicalLineModel(5, 3)
	open.write("abcde")
	if got := open.sealedTexts(); len(got) != 0 {
		t.Fatalf("expected exact-width write without LF to stay open, got %#v", got)
	}
	if got := open.openText(); got != "abcde" {
		t.Fatalf("expected open exact-width line, got %q", got)
	}

	sealed := newLogicalLineModel(5, 3)
	sealed.write("abcde")
	sealed.lineFeed()
	if got := sealed.sealedTexts(); !reflect.DeepEqual(got, []string{"abcde"}) {
		t.Fatalf("expected LF to seal exact-width line, got %#v", got)
	}
	if got := sealed.openText(); got != "" {
		t.Fatalf("expected empty next open line after LF, got %q", got)
	}
}

func TestLogicalLineModelCarriageReturnOverwritesCurrentVisualRow(t *testing.T) {
	m := newLogicalLineModel(5, 3)
	m.write("abcdefghij")
	m.carriageReturn()
	m.write("XY")

	if got := m.openText(); got != "abcdeXYhij" {
		t.Fatalf("expected CR to target current visual row start, got %q", got)
	}
	if got := m.projectAllRows(); !reflect.DeepEqual(got, []string{"abcde", "XYhij"}) {
		t.Fatalf("expected overwritten projection, got %#v", got)
	}
}

func TestLogicalLineModelEraseInsertDeleteStayWithinCurrentVisualRow(t *testing.T) {
	t.Run("erase-to-eol", func(t *testing.T) {
		m := newLogicalLineModel(5, 3)
		m.write("abcdefghij")
		m.carriageReturn()
		m.eraseToEOL()
		if got := m.openText(); got != "abcde     " {
			t.Fatalf("expected erase to preserve blanks in logical truth, got %q", got)
		}
		if got := m.projectAllRows(); !reflect.DeepEqual(got, []string{"abcde", "     "}) {
			t.Fatalf("expected projected blank row after erase, got %#v", got)
		}
	})

	t.Run("delete-chars", func(t *testing.T) {
		m := newLogicalLineModel(6, 3)
		m.write("abcdef")
		m.carriageReturn()
		m.deleteChars(2)
		if got := m.openText(); got != "cdef  " {
			t.Fatalf("expected DCH-like shift-left semantics, got %q", got)
		}
	})

	t.Run("erase-chars", func(t *testing.T) {
		m := newLogicalLineModel(6, 3)
		m.write("abcdef")
		m.carriageReturn()
		m.moveCursorCol(2)
		m.eraseChars(2)
		if got := m.openText(); got != "ab  ef" {
			t.Fatalf("expected ECH-like blanking semantics, got %q", got)
		}
	})

	t.Run("insert-chars", func(t *testing.T) {
		m := newLogicalLineModel(6, 3)
		m.write("abcdef")
		m.carriageReturn()
		m.moveCursorCol(1)
		m.insertChars(2)
		if got := m.openText(); got != "a  bcd" {
			t.Fatalf("expected ICH-like shift-right within row semantics, got %q", got)
		}
	})
}

func TestLogicalLineModelOpenLineCanOverflowScreenTopWithoutFlushing(t *testing.T) {
	m := newLogicalLineModel(4, 2)
	m.write("abcdefghij")

	if got := m.sealedTexts(); len(got) != 0 {
		t.Fatalf("expected open line not to flush just because it overflowed visible top, got %#v", got)
	}
	if got := m.openText(); got != "abcdefghij" {
		t.Fatalf("expected open line truth preserved, got %q", got)
	}
	if got := m.visibleRows(); !reflect.DeepEqual(got, []string{"efgh", "ij"}) {
		t.Fatalf("expected visible rows to be last projected rows only, got %#v", got)
	}
}

func TestLogicalLineModelResizeDoesNotRewriteSealedOrOpenTruth(t *testing.T) {
	m := newLogicalLineModel(5, 4)
	m.write("abcdefghij")
	m.lineFeed()
	m.write("klmnopqrst")

	if got := m.sealedTexts(); !reflect.DeepEqual(got, []string{"abcdefghij"}) {
		t.Fatalf("expected sealed truth before resize, got %#v", got)
	}
	if got := m.openText(); got != "klmnopqrst" {
		t.Fatalf("expected open truth before resize, got %q", got)
	}

	m.resize(4, 4)
	if got := m.sealedTexts(); !reflect.DeepEqual(got, []string{"abcdefghij"}) {
		t.Fatalf("expected sealed truth unchanged after narrow resize, got %#v", got)
	}
	if got := m.openText(); got != "klmnopqrst" {
		t.Fatalf("expected open truth unchanged after narrow resize, got %q", got)
	}
	if got := m.projectAllRows(); !reflect.DeepEqual(got, []string{"abcd", "efgh", "ij", "klmn", "opqr", "st"}) {
		t.Fatalf("unexpected narrow projection %#v", got)
	}

	m.resize(7, 4)
	if got := m.sealedTexts(); !reflect.DeepEqual(got, []string{"abcdefghij"}) {
		t.Fatalf("expected sealed truth unchanged after wide resize, got %#v", got)
	}
	if got := m.openText(); got != "klmnopqrst" {
		t.Fatalf("expected open truth unchanged after wide resize, got %q", got)
	}
	if got := m.projectAllRows(); !reflect.DeepEqual(got, []string{"abcdefg", "hij", "klmnopq", "rst"}) {
		t.Fatalf("unexpected wide projection %#v", got)
	}
}

func TestLogicalLineModelSealedHistoryAndVisibleProjection(t *testing.T) {
	m := newLogicalLineModel(4, 2)
	m.write("one")
	m.lineFeed()
	m.write("two")
	m.lineFeed()
	m.write("abcdef")

	if got := m.sealedTexts(); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("expected sealed history lines, got %#v", got)
	}
	if got := m.visibleRows(); !reflect.DeepEqual(got, []string{"abcd", "ef"}) {
		t.Fatalf("expected visible rows to follow latest open logical line projection, got %#v", got)
	}
}
