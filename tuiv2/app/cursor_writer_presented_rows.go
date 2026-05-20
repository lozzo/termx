package app

import (
	"strconv"
	"strings"
	"unicode/utf8"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/shared"
)

func rowOwnsLineEnd(row presentedRow) bool {
	if row.raw == "" {
		return false
	}
	return strings.Contains(row.raw, "\x1b[K")
}

const presentedStyledBlankECHThreshold = 5
const framePresenterCellPoolMaxSlices = 256

type presentedCellAcquirer func(int) []presentedCell

func joinFrameLinesForHost(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var out strings.Builder
	out.Grow(joinedLinesLen(lines))
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(normalizeFrameLineForHost(line))
	}
	return out.String()
}

func normalizeFrameLineForHost(line string) string {
	if line == "" || !strings.Contains(line, "\x1b[") || !strings.Contains(line, " ") {
		return line
	}
	original := line
	line = strings.TrimSuffix(line, xansi.EraseLineRight)
	row := parsePresentedRow(line)
	defer releasePresentedCells(row.cells)
	if row.hasHiddenEmojiCompensation || row.hasHostWidthStabilizer || !presentedCellsHaveStyledBlankRun(row.cells) {
		return original
	}
	var out strings.Builder
	out.Grow(len(line))
	writePresentedCells(&out, row.cells, 1)
	return out.String()
}

func presentedCellsHaveStyledBlankRun(cells []presentedCell) bool {
	for i := 0; i < len(cells); {
		run := presentedStyledBlankRun(cells, i)
		if run >= presentedStyledBlankECHThreshold {
			return true
		}
		if run > 0 {
			i += run
			continue
		}
		i++
	}
	return false
}

func writeOwnedLineEndClear(out *strings.Builder, style presentedStyle) {
	if out == nil {
		return
	}
	if style != (presentedStyle{}) {
		out.WriteString(presentedStyleDiffANSI(presentedStyle{}, style))
	}
	writeECH(out, 1)
	if style != (presentedStyle{}) {
		out.WriteString(presentedResetStyleSequence)
	}
}

func parsePresentedRow(row string) presentedRow {
	return parsePresentedRowWith(row, acquirePresentedCells)
}

func (p *framePresenter) parsePresentedRow(row string) presentedRow {
	if p == nil {
		return parsePresentedRow(row)
	}
	return parsePresentedRowWith(row, p.acquirePresentedCells)
}

func parsePresentedRowWith(row string, acquire presentedCellAcquirer) presentedRow {
	if row == "" {
		return presentedRow{raw: row}
	}
	if fast, ok := parsePresentedRowASCIIWith(row, acquire); ok {
		return fast
	}
	return parsePresentedRowGenericWith(row, acquire)
}

func parsePresentedRowASCII(row string) (presentedRow, bool) {
	return parsePresentedRowASCIIWith(row, acquirePresentedCells)
}

func parsePresentedRowASCIIWith(row string, acquire presentedCellAcquirer) (presentedRow, bool) {
	capHint, ok := presentedRowASCIICellCapacity(row)
	if !ok {
		return presentedRow{}, false
	}
	style := presentedStyle{}
	cells := acquire(capHint)
	hasStyled := false
	hasErase := false
	fail := func() (presentedRow, bool) {
		releasePresentedCells(cells)
		return presentedRow{}, false
	}
	for i := 0; i < len(row); {
		b := row[i]
		if b == '\x1b' {
			if i+1 >= len(row) || row[i+1] != '[' {
				return fail()
			}
			j := i + 2
			for j < len(row) && (row[j] < '@' || row[j] > '~') {
				if row[j] >= utf8.RuneSelf {
					return fail()
				}
				j++
			}
			if j >= len(row) {
				return fail()
			}
			final := row[j]
			params := row[i+2 : j]
			switch final {
			case 'm':
				next, ok := presentedStyleWithSGRRaw(style, params)
				if !ok {
					return fail()
				}
				style = next
			case 'X':
				count, ok := parseCSIIntASCII(params, 1)
				if !ok {
					return fail()
				}
				hasErase = true
				if style != (presentedStyle{}) {
					hasStyled = true
				}
				for k := 0; k < count; k++ {
					cells = append(cells, presentedCell{Content: " ", Width: 1, Style: style, Erase: true})
				}
			}
			i = j + 1
			continue
		}
		if b >= utf8.RuneSelf || b < 0x20 || b == 0x7f {
			return fail()
		}
		if style != (presentedStyle{}) {
			hasStyled = true
		}
		cells = append(cells, presentedCell{Content: row[i : i+1], Width: 1, Style: style})
		i++
	}
	return presentedRow{raw: row, cells: cells, hasStyled: hasStyled, hasErase: hasErase}, true
}

func presentedRowASCIICellCapacity(row string) (int, bool) {
	cells := 0
	for i := 0; i < len(row); {
		b := row[i]
		if b == '\x1b' {
			if i+1 >= len(row) || row[i+1] != '[' {
				return 0, false
			}
			j := i + 2
			for j < len(row) && (row[j] < '@' || row[j] > '~') {
				if row[j] >= utf8.RuneSelf {
					return 0, false
				}
				j++
			}
			if j >= len(row) {
				return 0, false
			}
			if row[j] == 'X' {
				count, ok := parseCSIIntASCII(row[i+2:j], 1)
				if !ok {
					return 0, false
				}
				cells += count
			}
			i = j + 1
			continue
		}
		if b >= utf8.RuneSelf || b < 0x20 || b == 0x7f {
			return 0, false
		}
		cells++
		i++
	}
	return cells, true
}

func parsePresentedRowGeneric(row string) presentedRow {
	return parsePresentedRowGenericWith(row, acquirePresentedCells)
}

func parsePresentedRowGenericWith(row string, acquire presentedCellAcquirer) presentedRow {
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)
	state := byte(0)
	rest := row
	style := presentedStyle{}
	cells := acquire(0)
	hasStyled := false
	hasWide := false
	hasErase := false
	hasHiddenEmojiCompensation := false
	hasHostWidthStabilizer := false
	widthSafety := shared.WidthSafetyTracker{}
	for len(rest) > 0 {
		seq, width, n, nextState := xansi.DecodeSequence(rest, state, parser)
		if n <= 0 {
			break
		}
		token := string(seq)
		if width > 0 {
			transition := widthSafety.ObserveDisplayedCluster(token, width)
			if style != (presentedStyle{}) {
				hasStyled = true
			}
			if width != 1 {
				hasWide = true
			}
			cells = append(cells, presentedCell{Content: token, Width: width, Style: style, ReanchorBefore: transition.ReanchorBefore})
		} else if len(token) > 0 && token[0] != '\x1b' {
			widthSafety.ObserveNonPrintingCluster(token, width)
		} else if len(token) > 0 && token[0] == '\x1b' {
			switch xansi.Cmd(parser.Command()).Final() {
			case 'm':
				style = style.withSGRRaw(token, parser.Params())
			case 'G':
				transition := widthSafety.ObserveReanchorBeforeNextCluster()
				if transition.HostWidthStabilizer {
					hasHostWidthStabilizer = true
				}
			case 'X':
				count, ok := parser.Param(0, 1)
				if !ok || count <= 0 {
					count = 1
				}
				transition := widthSafety.ObserveErase(count)
				if transition.HiddenCompensation {
					hasHiddenEmojiCompensation = true
				}
				hasErase = true
				if style != (presentedStyle{}) {
					hasStyled = true
				}
				for i := 0; i < count; i++ {
					cells = append(cells, presentedCell{Content: " ", Width: 1, Style: style, Erase: true, ReanchorBefore: transition.ReanchorBefore && i == 0})
				}
			}
		}
		state = nextState
		rest = rest[n:]
	}
	return presentedRow{
		raw:                        row,
		cells:                      cells,
		hasStyled:                  hasStyled,
		hasWide:                    hasWide,
		hasErase:                   hasErase,
		hasHiddenEmojiCompensation: hasHiddenEmojiCompensation,
		hasHostWidthStabilizer:     hasHostWidthStabilizer,
	}
}

func acquirePresentedCells(capHint int) []presentedCell {
	if capHint < 0 {
		capHint = 0
	}
	if pooled, ok := presentedCellPool.Get().([]presentedCell); ok {
		if cap(pooled) >= capHint {
			return pooled[:0]
		}
		releasePresentedCells(pooled)
	}
	return make([]presentedCell, 0, capHint)
}

func (p *framePresenter) acquirePresentedCells(capHint int) []presentedCell {
	if p == nil {
		return acquirePresentedCells(capHint)
	}
	if capHint < 0 {
		capHint = 0
	}
	best := -1
	for i, cells := range p.cellPool {
		if cap(cells) < capHint {
			continue
		}
		if best < 0 || cap(cells) < cap(p.cellPool[best]) {
			best = i
		}
	}
	if best >= 0 {
		cells := p.cellPool[best]
		last := len(p.cellPool) - 1
		p.cellPool[best] = p.cellPool[last]
		p.cellPool[last] = nil
		p.cellPool = p.cellPool[:last]
		return cells[:0]
	}
	return acquirePresentedCells(capHint)
}

func releasePresentedRows(rows []presentedRow) {
	for i := range rows {
		releasePresentedCells(rows[i].cells)
		rows[i] = presentedRow{}
	}
}

func (p *framePresenter) releasePresentedRows(rows []presentedRow) {
	if p == nil {
		releasePresentedRows(rows)
		return
	}
	for i := range rows {
		p.releasePresentedCells(rows[i].cells)
		rows[i] = presentedRow{}
	}
}

func releasePresentedCellSlices(cells [][]presentedCell) {
	for _, cellSlice := range cells {
		releasePresentedCells(cellSlice)
	}
}

func (p *framePresenter) releasePresentedCellSlices(cells [][]presentedCell) {
	if p == nil {
		releasePresentedCellSlices(cells)
		return
	}
	for _, cellSlice := range cells {
		p.releasePresentedCells(cellSlice)
	}
}

func releasePresentedCells(cells []presentedCell) {
	if len(cells) == 0 || cap(cells) > maxPooledPresentedCellCapacity {
		return
	}
	clear(cells)
	presentedCellPool.Put(cells[:0])
}

func (p *framePresenter) releasePresentedCells(cells []presentedCell) {
	if len(cells) == 0 {
		return
	}
	if p == nil || cap(cells) > maxPooledPresentedCellCapacity || len(p.cellPool) >= framePresenterCellPoolMaxSlices {
		releasePresentedCells(cells)
		return
	}
	clear(cells)
	p.cellPool = append(p.cellPool, cells[:0])
}

func parseCSIIntASCII(raw string, def int) (int, bool) {
	if raw == "" {
		return def, true
	}
	value := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == ';' {
			break
		}
		if raw[i] < '0' || raw[i] > '9' {
			return def, false
		}
		value = value*10 + int(raw[i]-'0')
	}
	return value, true
}

func (s presentedStyle) withSGRASCII(raw string) (presentedStyle, bool) {
	var local [16]int
	params, ok := parseSGRRawParams(raw, local[:0])
	if !ok {
		return presentedStyle{}, false
	}
	return s.withSGRInts(params), true
}

func parseSGRRawParams(raw string, dst []int) ([]int, bool) {
	if raw == "" {
		return append(dst, 0), true
	}
	for start := 0; start <= len(raw); {
		end := start
		for end < len(raw) && raw[end] != ';' {
			end++
		}
		group := raw[start:end]
		var ok bool
		if strings.Contains(group, ":") {
			dst, ok = appendColonSGRGroup(dst, group)
		} else {
			dst, ok = appendSGRIntParam(dst, group)
		}
		if !ok {
			return nil, false
		}
		if end == len(raw) {
			break
		}
		start = end + 1
	}
	return dst, true
}

func appendSGRIntParam(dst []int, raw string) ([]int, bool) {
	if raw == "" {
		return append(dst, 0), true
	}
	value := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return nil, false
		}
		value = value*10 + int(raw[i]-'0')
	}
	return append(dst, value), true
}

func appendColonSGRGroup(dst []int, raw string) ([]int, bool) {
	var local [8]int
	values := local[:0]
	for start := 0; start <= len(raw); {
		end := start
		for end < len(raw) && raw[end] != ':' {
			end++
		}
		if end == start {
			values = append(values, -1)
		} else {
			value := 0
			for i := start; i < end; i++ {
				if raw[i] < '0' || raw[i] > '9' {
					return nil, false
				}
				value = value*10 + int(raw[i]-'0')
			}
			values = append(values, value)
		}
		if end == len(raw) {
			break
		}
		start = end + 1
	}
	if len(values) == 0 || values[0] < 0 {
		return append(dst, 0), true
	}
	if (values[0] == 38 || values[0] == 48) && len(values) >= 3 {
		switch values[1] {
		case 5:
			if values[2] < 0 {
				return nil, false
			}
			return append(dst, values[0], 5, values[2]), true
		case 2:
			start := 2
			if len(values) >= 6 {
				start = 3
			}
			if start < len(values) && values[start] < 0 {
				start++
			}
			if start+2 >= len(values) || values[start] < 0 || values[start+1] < 0 || values[start+2] < 0 {
				return nil, false
			}
			return append(dst, values[0], 2, values[start], values[start+1], values[start+2]), true
		}
	}
	return append(dst, values[0]), true
}

func writePresentedCells(out *strings.Builder, cells []presentedCell, startCol int) presentedStyle {
	if out == nil || len(cells) == 0 {
		return presentedStyle{}
	}
	current := presentedStyle{}
	first := true
	cursorCol := maxInt(1, startCol)
	needsReanchor := false
	for i := 0; i < len(cells); {
		cell := cells[i]
		if needsReanchor || cell.ReanchorBefore {
			writeCHA(out, cursorCol)
			needsReanchor = false
		}
		if first || cell.Style != current {
			out.WriteString(presentedStyleDiffANSI(current, cell.Style))
			current = cell.Style
			first = false
		}
		if cell.Erase {
			writeECH(out, maxInt(1, cell.Width))
			cursorCol += maxInt(1, cell.Width)
			needsReanchor = true
			i++
			continue
		}
		if blankRun := presentedStyledBlankRun(cells, i); blankRun >= presentedStyledBlankECHThreshold {
			writeECH(out, blankRun)
			cursorCol += blankRun
			needsReanchor = true
			i += blankRun
			continue
		}
		out.WriteString(cell.Content)
		cursorCol += maxInt(1, cell.Width)
		i++
	}
	if needsReanchor {
		writeCHA(out, cursorCol)
	}
	if current != (presentedStyle{}) {
		out.WriteString(presentedResetStyleSequence)
	}
	return current
}

func presentedStyledBlankRun(cells []presentedCell, start int) int {
	if start < 0 || start >= len(cells) {
		return 0
	}
	style := cells[start].Style
	if style == (presentedStyle{}) {
		return 0
	}
	run := 0
	for i := start; i < len(cells); i++ {
		cell := cells[i]
		if i > start && cell.ReanchorBefore {
			break
		}
		if cell.Erase || cell.Style != style || cell.Width != 1 || (cell.Content != "" && cell.Content != " ") {
			break
		}
		run++
	}
	return run
}

func writeCUP(out *strings.Builder, col, row int) {
	writeCSI(out, 'H', row, col)
}

func writeCHA(out *strings.Builder, col int) {
	writeCSI(out, 'G', col)
}

func writeECH(out *strings.Builder, count int) {
	writeCSI(out, 'X', count)
}

func writeICH(out *strings.Builder, count int) {
	writeCSI(out, '@', count)
}

func writeDCH(out *strings.Builder, count int) {
	writeCSI(out, 'P', count)
}

func writeEL(out *strings.Builder, mode int) {
	writeCSI(out, 'K', mode)
}

func writeCSI(out *strings.Builder, final byte, params ...int) {
	if out == nil {
		return
	}
	out.WriteByte('\x1b')
	out.WriteByte('[')
	for i, param := range params {
		if i > 0 {
			out.WriteByte(';')
		}
		writeBuilderInt(out, param)
	}
	out.WriteByte(final)
}

func writeBuilderInt(out *strings.Builder, value int) {
	if out == nil {
		return
	}
	var scratch [24]byte
	buf := strconv.AppendInt(scratch[:0], int64(value), 10)
	_, _ = out.Write(buf)
}

type presentedStyleTransitionKey struct {
	From presentedStyle
	To   presentedStyle
}

type presentedStyleSGRKey struct {
	From presentedStyle
	Raw  string
}

func presentedStyleWithSGRRaw(s presentedStyle, raw string) (presentedStyle, bool) {
	key := presentedStyleSGRKey{From: s, Raw: raw}
	presentedSGRCache.mu.RLock()
	cached, ok := presentedSGRCache.m[key]
	presentedSGRCache.mu.RUnlock()
	if ok {
		return cached, true
	}
	next, ok := s.withSGRASCII(raw)
	if !ok {
		return presentedStyle{}, false
	}
	presentedSGRCache.mu.Lock()
	presentedSGRCache.m[presentedStyleSGRKey{From: s, Raw: strings.Clone(raw)}] = next
	presentedSGRCache.mu.Unlock()
	return next, true
}

func presentedStyleDiffANSI(from, to presentedStyle) string {
	if from == to {
		return ""
	}
	key := presentedStyleTransitionKey{From: from, To: to}
	presentedStyleDiffCache.mu.RLock()
	cached, ok := presentedStyleDiffCache.m[key]
	presentedStyleDiffCache.mu.RUnlock()
	if ok {
		return cached
	}
	ansi := buildPresentedStyleDiffANSI(from, to)
	presentedStyleDiffCache.mu.Lock()
	presentedStyleDiffCache.m[key] = ansi
	presentedStyleDiffCache.mu.Unlock()
	return ansi
}

func buildPresentedStyleDiffANSI(from, to presentedStyle) string {
	if from == to {
		return ""
	}
	if to == (presentedStyle{}) {
		return presentedResetStyleSequence
	}
	var b strings.Builder
	b.WriteString("\x1b[")
	first := true
	appendPresentedStyleToggle(&b, &first, from.Bold, to.Bold, "1", "22")
	appendPresentedStyleToggle(&b, &first, from.Italic, to.Italic, "3", "23")
	appendPresentedStyleToggle(&b, &first, from.Underline, to.Underline, "4", "24")
	appendPresentedStyleToggle(&b, &first, from.Blink, to.Blink, "5", "25")
	appendPresentedStyleToggle(&b, &first, from.Reverse, to.Reverse, "7", "27")
	appendPresentedStyleToggle(&b, &first, from.Strikethrough, to.Strikethrough, "9", "29")
	if from.FGCode != to.FGCode {
		if to.FGCode == "" {
			appendPresentedStyleCode(&b, &first, "39")
		} else {
			appendPresentedStyleCode(&b, &first, to.FGCode)
		}
	}
	if from.BGCode != to.BGCode {
		if to.BGCode == "" {
			appendPresentedStyleCode(&b, &first, "49")
		} else {
			appendPresentedStyleCode(&b, &first, to.BGCode)
		}
	}
	if first {
		return ""
	}
	b.WriteByte('m')
	return b.String()
}

func appendPresentedStyleToggle(b *strings.Builder, first *bool, from, to bool, onCode, offCode string) {
	if from == to {
		return
	}
	if to {
		appendPresentedStyleCode(b, first, onCode)
		return
	}
	appendPresentedStyleCode(b, first, offCode)
}

func appendPresentedStyleCode(b *strings.Builder, first *bool, code string) {
	if b == nil || first == nil || code == "" {
		return
	}
	if !*first {
		b.WriteByte(';')
	}
	b.WriteString(code)
	*first = false
}

func (s presentedStyle) withSGRRaw(token string, params xansi.Params) presentedStyle {
	if len(token) < 3 || token[0] != '\x1b' || token[1] != '[' || token[len(token)-1] != 'm' {
		return s.withSGRParams(params)
	}
	next, ok := presentedStyleWithSGRRaw(s, token[2:len(token)-1])
	if !ok {
		return s.withSGRParams(params)
	}
	return next
}

func (s presentedStyle) withSGRParams(params xansi.Params) presentedStyle {
	if len(params) == 0 {
		return presentedStyle{}
	}
	var local [16]int
	values := local[:0]
	for i := 0; i < len(params); i++ {
		param, _, ok := params.Param(i, 0)
		if !ok {
			continue
		}
		values = append(values, param)
	}
	return s.withSGRInts(values)
}

func (s presentedStyle) withSGRInts(params []int) presentedStyle {
	if len(params) == 0 {
		return presentedStyle{}
	}
	next := s
	for i := 0; i < len(params); i++ {
		param := params[i]
		switch param {
		case 0:
			next = presentedStyle{}
		case 1:
			next.Bold = true
		case 3:
			next.Italic = true
		case 4:
			next.Underline = true
		case 5:
			next.Blink = true
		case 7:
			next.Reverse = true
		case 9:
			next.Strikethrough = true
		case 22:
			next.Bold = false
		case 23:
			next.Italic = false
		case 24:
			next.Underline = false
		case 25:
			next.Blink = false
		case 27:
			next.Reverse = false
		case 29:
			next.Strikethrough = false
		case 39:
			next.FGCode = ""
		case 49:
			next.BGCode = ""
		case 38, 48:
			if i+1 >= len(params) {
				continue
			}
			mode := params[i+1]
			switch mode {
			case 5:
				if i+2 >= len(params) {
					continue
				}
				value := params[i+2]
				code := strconv.Itoa(param) + ";5;" + strconv.Itoa(value)
				if param == 38 {
					next.FGCode = code
				} else {
					next.BGCode = code
				}
				i += 2
			case 2:
				if i+4 >= len(params) {
					continue
				}
				r := params[i+2]
				g := params[i+3]
				b := params[i+4]
				code := strconv.Itoa(param) + ";2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b)
				if param == 38 {
					next.FGCode = code
				} else {
					next.BGCode = code
				}
				i += 4
			}
		default:
			switch {
			case 30 <= param && param <= 37:
				next.FGCode = ansiSimpleColorCode(param)
			case 90 <= param && param <= 97:
				next.FGCode = ansiSimpleColorCode(param)
			case 40 <= param && param <= 47:
				next.BGCode = ansiSimpleColorCode(param)
			case 100 <= param && param <= 107:
				next.BGCode = ansiSimpleColorCode(param)
			}
		}
	}
	return next
}

func ansiSimpleColorCode(param int) string {
	switch param {
	case 30:
		return "30"
	case 31:
		return "31"
	case 32:
		return "32"
	case 33:
		return "33"
	case 34:
		return "34"
	case 35:
		return "35"
	case 36:
		return "36"
	case 37:
		return "37"
	case 40:
		return "40"
	case 41:
		return "41"
	case 42:
		return "42"
	case 43:
		return "43"
	case 44:
		return "44"
	case 45:
		return "45"
	case 46:
		return "46"
	case 47:
		return "47"
	case 90:
		return "90"
	case 91:
		return "91"
	case 92:
		return "92"
	case 93:
		return "93"
	case 94:
		return "94"
	case 95:
		return "95"
	case 96:
		return "96"
	case 97:
		return "97"
	case 100:
		return "100"
	case 101:
		return "101"
	case 102:
		return "102"
	case 103:
		return "103"
	case 104:
		return "104"
	case 105:
		return "105"
	case 106:
		return "106"
	case 107:
		return "107"
	default:
		return strconv.Itoa(param)
	}
}
