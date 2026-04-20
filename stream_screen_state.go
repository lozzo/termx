package termx

import (
	"strings"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

type streamScreenState struct {
	snapshot *protocol.Snapshot
	title    string
}

type screenUpdateEncodeMode string

const (
	screenUpdateEncodeModeDelta       screenUpdateEncodeMode = "delta"
	screenUpdateEncodeModeFullReplace screenUpdateEncodeMode = "full_replace"
)

const (
	backlogAlternateScreenCollapseFrames = 2
	backlogNormalScreenCollapseFrames    = 4
	backlogAlternateScreenCollapseBytes  = 16 * 1024
	backlogNormalScreenCollapseBytes     = 64 * 1024
)

func cloneStreamScreenState(state *streamScreenState) *streamScreenState {
	if state == nil {
		return nil
	}
	return &streamScreenState{
		snapshot: cloneProtocolSnapshot(state.snapshot),
		title:    state.title,
	}
}

func applyStreamScreenUpdateState(current *streamScreenState, terminalID string, update protocol.ScreenUpdate) *streamScreenState {
	next := cloneStreamScreenState(current)
	if next == nil {
		next = &streamScreenState{}
	}
	next.snapshot = applyScreenUpdateSnapshot(next.snapshot, terminalID, update)
	if update.Title != "" {
		next.title = update.Title
	}
	return next
}

func resizeStreamScreenState(current *streamScreenState, terminalID string, cols, rows uint16) *streamScreenState {
	next := cloneStreamScreenState(current)
	if next == nil {
		next = &streamScreenState{
			snapshot: &protocol.Snapshot{
				TerminalID: terminalID,
				Size:       protocol.Size{Cols: cols, Rows: rows},
				Screen: protocol.ScreenData{
					Cells: make([][]protocol.Cell, rows),
				},
				ScreenTimestamps: make([]time.Time, rows),
				ScreenRowKinds:   make([]string, rows),
				Cursor:           protocol.CursorState{Visible: true},
				Modes:            protocol.TerminalModes{AutoWrap: true},
				Timestamp:        time.Now().UTC(),
			},
		}
		return next
	}
	next.snapshot = resizeProtocolSnapshot(next.snapshot, cols, rows)
	return next
}

func encodeMergedScreenStatePayload(before, after *streamScreenState, resetScrollback bool) ([]byte, bool) {
	if after == nil || after.snapshot == nil {
		return nil, false
	}
	if !resetScrollback && !after.snapshot.Modes.AlternateScreen {
		resetScrollback = true
	}
	update := fullReplaceUpdateForStateDelta(before, after, resetScrollback)
	payload, err := protocol.EncodeScreenUpdatePayload(update)
	if err != nil {
		return nil, false
	}
	perftrace.Count("terminal.screen_update.encoded_bytes", len(payload))
	perftrace.Count("terminal.screen_update.encode_mode.full_replace", len(payload))
	return payload, true
}

func encodeScreenUpdatePayloadByStrategy(damage protocol.ScreenUpdate, full protocol.ScreenUpdate, preferAggressiveFullReplace bool) ([]byte, screenUpdateEncodeMode, bool) {
	damagePayload, damageErr := protocol.EncodeScreenUpdatePayload(damage)
	fullPayload, fullErr := protocol.EncodeScreenUpdatePayload(full)
	switch {
	case damageErr != nil && fullErr != nil:
		return nil, "", false
	case damageErr != nil:
		perftrace.Count("terminal.screen_update.encoded_bytes", len(fullPayload))
		perftrace.Count("terminal.screen_update.encode_mode.full_replace", len(fullPayload))
		return fullPayload, screenUpdateEncodeModeFullReplace, true
	case fullErr != nil:
		perftrace.Count("terminal.screen_update.encoded_bytes", len(damagePayload))
		perftrace.Count("terminal.screen_update.encode_mode.delta", len(damagePayload))
		return damagePayload, screenUpdateEncodeModeDelta, true
	}

	damageChangedRows := screenUpdateChangedRowCount(damage)
	damageChangedCells := screenUpdateChangedCellCount(damage)
	totalCells := screenUpdateTotalCells(full)
	changedRatio := 0.0
	if totalCells > 0 {
		changedRatio = float64(damageChangedCells) / float64(totalCells)
	}
	hasScrollOpcode := screenUpdateHasScrollOpcode(damage)
	hasScrollbackChange := damage.ResetScrollback || damage.ScrollbackTrim > 0 || len(damage.ScrollbackAppend) > 0
	chooseFull := false
	switch {
	case preferAggressiveFullReplace && len(fullPayload) <= len(damagePayload)+(len(damagePayload)/6):
		chooseFull = true
	case !hasScrollbackChange && changedRatio >= 0.60 && len(fullPayload) <= len(damagePayload):
		chooseFull = true
	case !hasScrollbackChange && damageChangedRows >= maxInt(1, len(full.Screen.Cells)*3/4) && len(fullPayload) <= len(damagePayload)+(len(damagePayload)/10):
		chooseFull = true
	case !hasScrollOpcode && len(fullPayload) < len(damagePayload):
		chooseFull = true
	}
	if hasScrollOpcode && len(damagePayload) <= len(fullPayload)+(len(fullPayload)/8) {
		chooseFull = false
	}

	if chooseFull {
		perftrace.Count("terminal.screen_update.encoded_bytes", len(fullPayload))
		perftrace.Count("terminal.screen_update.encode_mode.full_replace", len(fullPayload))
		return fullPayload, screenUpdateEncodeModeFullReplace, true
	}
	perftrace.Count("terminal.screen_update.encoded_bytes", len(damagePayload))
	perftrace.Count("terminal.screen_update.encode_mode.delta", len(damagePayload))
	return damagePayload, screenUpdateEncodeModeDelta, true
}

func fullReplaceUpdateForStateDelta(before, after *streamScreenState, resetScrollback bool) protocol.ScreenUpdate {
	if after == nil || after.snapshot == nil {
		return protocol.ScreenUpdate{}
	}
	var beforeSnapshot *protocol.Snapshot
	if before != nil {
		beforeSnapshot = before.snapshot
	}
	scrollbackTrim, scrollbackAppend := protocolScrollbackDelta(beforeSnapshot, after.snapshot)
	update := protocol.ScreenUpdate{
		FullReplace:      true,
		ResetScrollback:  resetScrollback || beforeSnapshot == nil,
		Size:             after.snapshot.Size,
		Title:            after.title,
		Screen:           cloneProtocolScreenData(after.snapshot.Screen),
		ScreenTimestamps: append([]time.Time(nil), after.snapshot.ScreenTimestamps...),
		ScreenRowKinds:   append([]string(nil), after.snapshot.ScreenRowKinds...),
		ScrollbackTrim:   scrollbackTrim,
		ScrollbackAppend: scrollbackAppend,
		Cursor:           after.snapshot.Cursor,
		Modes:            after.snapshot.Modes,
	}
	if update.ResetScrollback {
		update.ScrollbackTrim = 0
		update.ScrollbackAppend = snapshotScrollbackRows(after.snapshot)
	}
	return update
}

func screenUpdateFromDamageState(damage localvterm.WriteDamage, after *streamScreenState) protocol.ScreenUpdate {
	update := protocol.ScreenUpdate{
		Size:             protocol.Size{Cols: uint16(damage.SizeCols), Rows: uint16(damage.SizeRows)},
		ScreenScroll:     damage.ScreenScroll,
		ChangedSpans:     make([]protocol.ScreenSpanUpdate, 0, len(damage.ChangedScreenSpans)),
		Ops:              make([]protocol.ScreenOp, 0, len(damage.Ops)),
		ScrollbackTrim:   damage.ScrollbackTrim,
		ScrollbackAppend: make([]protocol.ScrollbackRowAppend, 0, len(damage.ScrollbackAppend)),
		Cursor:           protocolCursorStateFromVTerm(damage.Cursor),
		Modes:            protocolModesFromVTerm(damage.Modes),
	}
	if after != nil {
		update.Title = after.title
	}
	for _, span := range damage.ChangedScreenSpans {
		update.ChangedSpans = append(update.ChangedSpans, protocol.ScreenSpanUpdate{
			Row:       span.Row,
			ColStart:  span.ColStart,
			Cells:     protocolCellsFromVTermRow(span.Cells),
			Op:        span.Op,
			Timestamp: span.Timestamp,
			RowKind:   span.RowKind,
		})
	}
	for _, op := range damage.Ops {
		update.Ops = append(update.Ops, protocol.ScreenOp{
			Code:      op.Code,
			Rect:      protocol.ScreenRect{X: op.Rect.X, Y: op.Rect.Y, Width: op.Rect.Width, Height: op.Rect.Height},
			Src:       protocol.ScreenRect{X: op.Src.X, Y: op.Src.Y, Width: op.Src.Width, Height: op.Src.Height},
			DstX:      op.DstX,
			DstY:      op.DstY,
			Dx:        op.Dx,
			Dy:        op.Dy,
			Row:       op.Row,
			Col:       op.Col,
			Cells:     protocolCellsFromVTermRow(op.Cells),
			Timestamp: op.Timestamp,
			RowKind:   op.RowKind,
		})
	}
	for _, row := range damage.ScrollbackAppend {
		update.ScrollbackAppend = append(update.ScrollbackAppend, protocol.ScrollbackRowAppend{
			Cells:     protocolCellsFromVTermRow(row.Cells),
			Timestamp: row.Timestamp,
			RowKind:   row.RowKind,
		})
	}
	return update
}

func snapshotScrollbackRows(snapshot *protocol.Snapshot) []protocol.ScrollbackRowAppend {
	if snapshot == nil || len(snapshot.Scrollback) == 0 {
		return nil
	}
	out := make([]protocol.ScrollbackRowAppend, 0, len(snapshot.Scrollback))
	for i, row := range snapshot.Scrollback {
		out = append(out, protocol.ScrollbackRowAppend{
			Cells:     cloneProtocolCellRow(row),
			Timestamp: timeAtProtocol(snapshot.ScrollbackTimestamps, i),
			RowKind:   stringAtProtocol(snapshot.ScrollbackRowKinds, i),
		})
	}
	return out
}

func protocolScrollbackDelta(before, after *protocol.Snapshot) (int, []protocol.ScrollbackRowAppend) {
	if after == nil || len(after.Scrollback) == 0 {
		if before == nil {
			return 0, nil
		}
		return len(before.Scrollback), nil
	}
	beforeLen := 0
	if before != nil {
		beforeLen = len(before.Scrollback)
	}
	afterLen := len(after.Scrollback)
	maxOverlap := minInt(beforeLen, afterLen)
	overlap := 0
	for candidate := maxOverlap; candidate >= 0; candidate-- {
		if protocolScrollbackWindowEqual(before, beforeLen-candidate, after, 0, candidate) {
			overlap = candidate
			break
		}
	}
	trim := beforeLen - overlap
	appends := make([]protocol.ScrollbackRowAppend, 0, afterLen-overlap)
	for i := overlap; i < afterLen; i++ {
		appends = append(appends, protocol.ScrollbackRowAppend{
			Cells:     cloneProtocolCellRow(after.Scrollback[i]),
			Timestamp: timeAtProtocol(after.ScrollbackTimestamps, i),
			RowKind:   stringAtProtocol(after.ScrollbackRowKinds, i),
		})
	}
	return trim, appends
}

func protocolScrollbackWindowEqual(before *protocol.Snapshot, beforeStart int, after *protocol.Snapshot, afterStart int, count int) bool {
	for i := 0; i < count; i++ {
		if !protocolRowsAndMetaEqual(
			rowAtProtocol(before.Scrollback, beforeStart+i),
			timeAtProtocol(before.ScrollbackTimestamps, beforeStart+i),
			stringAtProtocol(before.ScrollbackRowKinds, beforeStart+i),
			rowAtProtocol(after.Scrollback, afterStart+i),
			timeAtProtocol(after.ScrollbackTimestamps, afterStart+i),
			stringAtProtocol(after.ScrollbackRowKinds, afterStart+i),
		) {
			return false
		}
	}
	return true
}

func protocolRowsAndMetaEqual(left []protocol.Cell, leftTS time.Time, leftKind string, right []protocol.Cell, rightTS time.Time, rightKind string) bool {
	if leftKind != rightKind || !leftTS.Equal(rightTS) {
		return false
	}
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func screenUpdateHasScrollOpcode(update protocol.ScreenUpdate) bool {
	if update.ScreenScroll != 0 {
		return true
	}
	for _, op := range update.Ops {
		if op.Code == protocol.ScreenOpScrollRect || op.Code == protocol.ScreenOpCopyRect {
			return true
		}
	}
	return false
}

func screenUpdateChangedRowCount(update protocol.ScreenUpdate) int {
	seen := make(map[int]struct{}, len(update.ChangedSpans)+len(update.Ops))
	for _, span := range update.ChangedSpans {
		seen[span.Row] = struct{}{}
	}
	for _, op := range update.Ops {
		switch op.Code {
		case protocol.ScreenOpWriteSpan, protocol.ScreenOpClearToEOL:
			seen[op.Row] = struct{}{}
		case protocol.ScreenOpScrollRect, protocol.ScreenOpClearRect:
			for row := op.Rect.Y; row < op.Rect.Y+op.Rect.Height; row++ {
				seen[row] = struct{}{}
			}
		case protocol.ScreenOpCopyRect:
			for row := op.DstY; row < op.DstY+op.Src.Height; row++ {
				seen[row] = struct{}{}
			}
		}
	}
	return len(seen)
}

func screenUpdateChangedCellCount(update protocol.ScreenUpdate) int {
	count := 0
	for _, span := range update.ChangedSpans {
		switch span.Op {
		case protocol.ScreenSpanOpClearToEOL:
			continue
		case protocol.ScreenSpanOpReplaceRow:
			count += len(trimTrailingProtocolCells(span.Cells))
		default:
			count += len(span.Cells)
		}
	}
	for _, op := range update.Ops {
		switch op.Code {
		case protocol.ScreenOpWriteSpan:
			count += len(op.Cells)
		case protocol.ScreenOpClearRect:
			count += maxInt(0, op.Rect.Width*op.Rect.Height)
		case protocol.ScreenOpClearToEOL:
			count++
		case protocol.ScreenOpScrollRect, protocol.ScreenOpCopyRect:
			count += maxInt(0, op.Rect.Width*op.Rect.Height)
		}
	}
	for _, row := range update.ScrollbackAppend {
		count += len(trimTrailingProtocolCells(row.Cells))
	}
	return count
}

func screenUpdateTotalCells(update protocol.ScreenUpdate) int {
	if update.Size.Cols > 0 && update.Size.Rows > 0 {
		return int(update.Size.Cols) * int(update.Size.Rows)
	}
	width := 0
	for _, row := range update.Screen.Cells {
		if len(row) > width {
			width = len(row)
		}
	}
	if width <= 0 {
		return 0
	}
	return width * len(update.Screen.Cells)
}

func trimTrailingProtocolCells(row []protocol.Cell) []protocol.Cell {
	last := -1
	for i, cell := range row {
		if protocolCellNeedsSnapshotRow(cell) {
			last = i
			if cell.Width > 1 {
				last = maxInt(last, minInt(len(row)-1, i+cell.Width-1))
			}
		}
	}
	if last < 0 {
		return nil
	}
	return row[:last+1]
}

func trimProtocolCellRow(row []protocol.Cell) []protocol.Cell {
	trimmed := trimTrailingProtocolCells(row)
	if len(trimmed) == 0 {
		return nil
	}
	return cloneProtocolCellRow(trimmed)
}

func protocolCellNeedsSnapshotRow(cell protocol.Cell) bool {
	if cell.Style != (protocol.CellStyle{}) {
		return true
	}
	if cell.Width > 1 {
		return true
	}
	if cell.Content == "" {
		return false
	}
	return strings.TrimSpace(cell.Content) != ""
}

func rowAtProtocol(rows [][]protocol.Cell, index int) []protocol.Cell {
	if index < 0 || index >= len(rows) {
		return nil
	}
	return rows[index]
}

func timeAtProtocol(values []time.Time, index int) time.Time {
	if index < 0 || index >= len(values) {
		return time.Time{}
	}
	return values[index]
}

func stringAtProtocol(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func resizeProtocolSnapshot(snapshot *protocol.Snapshot, cols, rows uint16) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	width := maxInt(1, int(maxUint16Local(snapshot.Size.Cols, cols)))
	height := maxInt(1, int(maxUint16Local(snapshot.Size.Rows, rows)))
	scrollbackSize := maxInt(len(snapshot.Scrollback)+height+1, 32)
	vt := localvterm.New(width, height, scrollbackSize, nil)
	vt.LoadSnapshotWithMetadata(
		protocolRowsToVTermRows(snapshot.Scrollback),
		append([]time.Time(nil), snapshot.ScrollbackTimestamps...),
		append([]string(nil), snapshot.ScrollbackRowKinds...),
		protocolScreenToVTerm(snapshot.Screen),
		append([]time.Time(nil), snapshot.ScreenTimestamps...),
		append([]string(nil), snapshot.ScreenRowKinds...),
		protocolCursorToVTerm(snapshot.Cursor),
		protocolModesToVTerm(snapshot.Modes),
	)
	vt.Resize(int(cols), int(rows))
	resized := snapshotFromVTerm(vt)
	resized.TerminalID = snapshot.TerminalID
	resized.Timestamp = time.Now().UTC()
	return resized
}

func snapshotFromVTerm(source *localvterm.VTerm) *protocol.Snapshot {
	if source == nil {
		return nil
	}
	screen := source.ScreenContent()
	scrollback := source.ScrollbackContent()
	screenRows := make([][]protocol.Cell, 0, len(screen.Cells))
	for _, row := range screen.Cells {
		screenRows = append(screenRows, protocolCellsFromVTermRow(row))
	}
	scrollbackRows := make([][]protocol.Cell, 0, len(scrollback))
	for _, row := range scrollback {
		scrollbackRows = append(scrollbackRows, protocolCellsFromVTermRow(row))
	}
	cols, rows := source.Size()
	return &protocol.Snapshot{
		Size:                 protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen:               protocolScreenDataFromVTerm(screen),
		Scrollback:           scrollbackRows,
		ScreenTimestamps:     append([]time.Time(nil), source.ScreenTimestamps()...),
		ScrollbackTimestamps: append([]time.Time(nil), source.ScrollbackTimestamps()...),
		ScreenRowKinds:       append([]string(nil), source.ScreenRowKinds()...),
		ScrollbackRowKinds:   append([]string(nil), source.ScrollbackRowKinds()...),
		Cursor:               protocolCursorStateFromVTerm(source.CursorState()),
		Modes:                protocolModesFromVTerm(source.Modes()),
		Timestamp:            time.Now().UTC(),
	}
}

func protocolRowsToVTermRows(rows [][]protocol.Cell) [][]localvterm.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]localvterm.Cell, len(rows))
	for y, row := range rows {
		out[y] = make([]localvterm.Cell, len(row))
		for x, cell := range row {
			out[y][x] = localvterm.Cell{
				Content: cell.Content,
				Width:   cell.Width,
				Style: localvterm.CellStyle{
					FG:            cell.Style.FG,
					BG:            cell.Style.BG,
					Bold:          cell.Style.Bold,
					Italic:        cell.Style.Italic,
					Underline:     cell.Style.Underline,
					Blink:         cell.Style.Blink,
					Reverse:       cell.Style.Reverse,
					Strikethrough: cell.Style.Strikethrough,
				},
			}
		}
	}
	return out
}

func protocolScreenToVTerm(screen protocol.ScreenData) localvterm.ScreenData {
	return localvterm.ScreenData{
		Cells:             protocolRowsToVTermRows(screen.Cells),
		IsAlternateScreen: screen.IsAlternateScreen,
	}
}

func protocolCursorToVTerm(cursor protocol.CursorState) localvterm.CursorState {
	return localvterm.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   localvterm.CursorShape(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesToVTerm(modes protocol.TerminalModes) localvterm.TerminalModes {
	return localvterm.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		AlternateScroll:   modes.AlternateScroll,
		MouseTracking:     modes.MouseTracking,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func applyScreenUpdateSnapshot(current *protocol.Snapshot, terminalID string, update protocol.ScreenUpdate) *protocol.Snapshot {
	update = protocol.NormalizeScreenUpdate(update)
	if update.FullReplace {
		snapshot := &protocol.Snapshot{TerminalID: terminalID}
		if update.Size.Cols > 0 || update.Size.Rows > 0 {
			snapshot.Size = update.Size
		}
		if update.ResetScrollback {
			snapshot.Scrollback = nil
			snapshot.ScrollbackTimestamps = nil
			snapshot.ScrollbackRowKinds = nil
		}
		snapshot.Screen = cloneProtocolScreenData(update.Screen)
		snapshot.ScreenTimestamps = append([]time.Time(nil), update.ScreenTimestamps...)
		snapshot.ScreenRowKinds = append([]string(nil), update.ScreenRowKinds...)
		for _, row := range update.ScrollbackAppend {
			snapshot.Scrollback = append(snapshot.Scrollback, cloneProtocolCellRow(row.Cells))
			snapshot.ScrollbackTimestamps = append(snapshot.ScrollbackTimestamps, row.Timestamp)
			snapshot.ScrollbackRowKinds = append(snapshot.ScrollbackRowKinds, row.RowKind)
		}
		snapshot.Screen.IsAlternateScreen = update.Modes.AlternateScreen
		snapshot.Cursor = update.Cursor
		snapshot.Modes = update.Modes
		snapshot.Timestamp = time.Now().UTC()
		return snapshot
	}

	snapshot := &protocol.Snapshot{TerminalID: terminalID}
	if current != nil {
		cloned := *current
		snapshot = &cloned
	}
	if update.Size.Cols > 0 || update.Size.Rows > 0 {
		snapshot.Size = update.Size
	}
	screenCellsOwned := false
	screenTimestampsOwned := false
	screenRowKindsOwned := false
	scrollbackOwned := false
	scrollbackTimestampsOwned := false
	scrollbackRowKindsOwned := false
	if update.ResetScrollback {
		snapshot.Scrollback = nil
		snapshot.ScrollbackTimestamps = nil
		snapshot.ScrollbackRowKinds = nil
		scrollbackOwned = true
		scrollbackTimestampsOwned = true
		scrollbackRowKindsOwned = true
	}
	requiredRows := int(maxUint16Local(snapshot.Size.Rows, uint16(maxChangedScreenRow(update)+1)))
	if requiredRows > len(snapshot.Screen.Cells) {
		ensureSnapshotScreenRowsCOW(snapshot, requiredRows, &screenCellsOwned, &screenTimestampsOwned, &screenRowKindsOwned)
	}
	if len(update.Ops) == 0 && update.ScreenScroll != 0 {
		shiftSnapshotScreenRows(snapshot, update.ScreenScroll, &screenCellsOwned, &screenTimestampsOwned, &screenRowKindsOwned)
	}
	if update.ScrollbackTrim > 0 {
		trimSnapshotScrollbackFront(snapshot, update.ScrollbackTrim)
		scrollbackOwned = true
		scrollbackTimestampsOwned = true
		scrollbackRowKindsOwned = true
	}
	screenRowCellsOwned := make(map[int]bool)
	if len(update.Ops) > 0 {
		applySnapshotScreenOps(snapshot, update, &screenCellsOwned, &screenTimestampsOwned, &screenRowKindsOwned, screenRowCellsOwned)
	} else {
		for _, span := range update.ChangedSpans {
			if span.Row < 0 {
				continue
			}
			ensureSnapshotScreenRowsCOW(snapshot, span.Row+1, &screenCellsOwned, &screenTimestampsOwned, &screenRowKindsOwned)
			ensureSnapshotScreenRowCellsCOW(snapshot, span.Row, &screenCellsOwned, screenRowCellsOwned)
			switch span.Op {
			case protocol.ScreenSpanOpClearToEOL:
				snapshot.Screen.Cells[span.Row] = clearProtocolCellRowFrom(snapshot.Screen.Cells[span.Row], span.ColStart)
			case protocol.ScreenSpanOpReplaceRow:
				snapshot.Screen.Cells[span.Row] = trimProtocolCellRow(cloneProtocolCellRow(span.Cells))
			default:
				snapshot.Screen.Cells[span.Row] = applyProtocolCellSpan(snapshot.Screen.Cells[span.Row], span.ColStart, span.Cells)
			}
			snapshot.ScreenTimestamps[span.Row] = span.Timestamp
			snapshot.ScreenRowKinds[span.Row] = span.RowKind
		}
	}
	if appendCount := len(update.ScrollbackAppend); appendCount > 0 {
		baseRows := len(snapshot.Scrollback)
		snapshot.Scrollback = cowProtocolRows(snapshot.Scrollback, baseRows+appendCount, &scrollbackOwned)
		snapshot.ScrollbackTimestamps = cowTimeSlice(snapshot.ScrollbackTimestamps, baseRows+appendCount, &scrollbackTimestampsOwned)
		snapshot.ScrollbackRowKinds = cowStringSlice(snapshot.ScrollbackRowKinds, baseRows+appendCount, &scrollbackRowKindsOwned)
		for i, row := range update.ScrollbackAppend {
			index := baseRows + i
			snapshot.Scrollback[index] = cloneProtocolCellRow(row.Cells)
			snapshot.ScrollbackTimestamps[index] = row.Timestamp
			snapshot.ScrollbackRowKinds[index] = row.RowKind
		}
	}
	snapshot.Screen.IsAlternateScreen = update.Modes.AlternateScreen
	snapshot.Cursor = update.Cursor
	snapshot.Modes = update.Modes
	snapshot.Timestamp = time.Now().UTC()
	return snapshot
}

func cloneProtocolSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen = cloneProtocolScreenData(snapshot.Screen)
	cloned.Scrollback = cloneProtocolRows(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	return &cloned
}

func cloneProtocolScreenData(screen protocol.ScreenData) protocol.ScreenData {
	return protocol.ScreenData{
		Cells:             cloneProtocolRows(screen.Cells),
		IsAlternateScreen: screen.IsAlternateScreen,
	}
}

func cloneProtocolRows(rows [][]protocol.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = cloneProtocolCellRow(row)
	}
	return out
}

func cloneProtocolCellRow(row []protocol.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	return append([]protocol.Cell(nil), row...)
}

func ensureSnapshotScreenRowCellsCOW(snapshot *protocol.Snapshot, row int, screenCellsOwned *bool, ownedRows map[int]bool) {
	if snapshot == nil || row < 0 {
		return
	}
	snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, row+1, screenCellsOwned)
	if ownedRows != nil {
		if ownedRows[row] {
			return
		}
		ownedRows[row] = true
	}
	snapshot.Screen.Cells[row] = cloneProtocolCellRow(snapshot.Screen.Cells[row])
}

func applyProtocolCellSpan(row []protocol.Cell, colStart int, cells []protocol.Cell) []protocol.Cell {
	if colStart < 0 {
		colStart = 0
	}
	if len(cells) == 0 {
		return trimProtocolCellRow(row)
	}
	row = padProtocolCellRow(row, colStart+len(cells))
	copy(row[colStart:], cells)
	return trimProtocolCellRow(row)
}

func clearProtocolCellRowFrom(row []protocol.Cell, colStart int) []protocol.Cell {
	if colStart <= 0 {
		return nil
	}
	if colStart >= len(row) {
		return trimProtocolCellRow(row)
	}
	return trimProtocolCellRow(cloneProtocolCellRow(row[:colStart]))
}

func applySnapshotScreenOps(snapshot *protocol.Snapshot, update protocol.ScreenUpdate, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned *bool, screenRowCellsOwned map[int]bool) {
	if snapshot == nil {
		return
	}
	for _, op := range update.Ops {
		switch op.Code {
		case protocol.ScreenOpWriteSpan:
			if op.Row < 0 {
				continue
			}
			ensureSnapshotScreenRowsCOW(snapshot, op.Row+1, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned)
			ensureSnapshotScreenRowCellsCOW(snapshot, op.Row, screenCellsOwned, screenRowCellsOwned)
			snapshot.Screen.Cells[op.Row] = applyProtocolCellSpan(snapshot.Screen.Cells[op.Row], op.Col, op.Cells)
			snapshot.ScreenTimestamps[op.Row] = op.Timestamp
			snapshot.ScreenRowKinds[op.Row] = op.RowKind
		case protocol.ScreenOpClearToEOL:
			if op.Row < 0 {
				continue
			}
			ensureSnapshotScreenRowsCOW(snapshot, op.Row+1, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned)
			ensureSnapshotScreenRowCellsCOW(snapshot, op.Row, screenCellsOwned, screenRowCellsOwned)
			snapshot.Screen.Cells[op.Row] = clearProtocolCellRowFrom(snapshot.Screen.Cells[op.Row], op.Col)
			snapshot.ScreenTimestamps[op.Row] = op.Timestamp
			snapshot.ScreenRowKinds[op.Row] = op.RowKind
		case protocol.ScreenOpClearRect:
			applySnapshotClearRect(snapshot, op, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenRowCellsOwned)
		case protocol.ScreenOpScrollRect:
			applySnapshotScrollRect(snapshot, op, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenRowCellsOwned)
		case protocol.ScreenOpCopyRect:
			applySnapshotCopyRect(snapshot, op, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenRowCellsOwned)
		case protocol.ScreenOpResize:
			rows := int(op.Size.Rows)
			if rows > 0 {
				ensureSnapshotScreenRowsCOW(snapshot, rows, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned)
			}
			if op.Size.Cols > 0 {
				snapshot.Size.Cols = op.Size.Cols
			}
			if op.Size.Rows > 0 {
				snapshot.Size.Rows = op.Size.Rows
			}
		}
	}
}

func applySnapshotClearRect(snapshot *protocol.Snapshot, op protocol.ScreenOp, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned *bool, screenRowCellsOwned map[int]bool) {
	rect := op.Rect
	if snapshot == nil || rect.Width <= 0 || rect.Height <= 0 || rect.Y < 0 {
		return
	}
	ensureSnapshotScreenRowsCOW(snapshot, rect.Y+rect.Height, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned)
	cols := snapshotScreenWidth(snapshot, rect.X+rect.Width)
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		ensureSnapshotScreenRowCellsCOW(snapshot, row, screenCellsOwned, screenRowCellsOwned)
		dense := padProtocolCellRow(snapshot.Screen.Cells[row], cols)
		for col := maxInt(rect.X, 0); col < minInt(rect.X+rect.Width, cols); col++ {
			dense[col] = protocolBlankCell()
		}
		snapshot.Screen.Cells[row] = trimProtocolCellRow(dense)
		snapshot.ScreenTimestamps[row] = op.Timestamp
		snapshot.ScreenRowKinds[row] = op.RowKind
	}
}

func applySnapshotScrollRect(snapshot *protocol.Snapshot, op protocol.ScreenOp, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned *bool, screenRowCellsOwned map[int]bool) {
	rect := op.Rect
	if snapshot == nil || rect.Width <= 0 || rect.Height <= 0 || rect.Y < 0 {
		return
	}
	ensureSnapshotScreenRowsCOW(snapshot, rect.Y+rect.Height, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned)
	cols := snapshotScreenWidth(snapshot, rect.X+rect.Width)
	fullWidth := op.Dx == 0 && rect.X == 0 && rect.Width >= cols
	if fullWidth {
		beforeRows := cloneProtocolRowsRect(snapshot.Screen.Cells, rect.Y, rect.Height)
		beforeTimes := append([]time.Time(nil), snapshot.ScreenTimestamps[rect.Y:rect.Y+rect.Height]...)
		beforeKinds := append([]string(nil), snapshot.ScreenRowKinds[rect.Y:rect.Y+rect.Height]...)
		for row := rect.Y; row < rect.Y+rect.Height; row++ {
			srcY := row - op.Dy
			if srcY >= rect.Y && srcY < rect.Y+rect.Height {
				snapshot.Screen.Cells[row] = beforeRows[srcY-rect.Y]
				snapshot.ScreenTimestamps[row] = beforeTimes[srcY-rect.Y]
				snapshot.ScreenRowKinds[row] = beforeKinds[srcY-rect.Y]
				markSnapshotScreenRowOwned(screenRowCellsOwned, row)
				continue
			}
			snapshot.Screen.Cells[row] = nil
			snapshot.ScreenTimestamps[row] = time.Time{}
			snapshot.ScreenRowKinds[row] = ""
			markSnapshotScreenRowOwned(screenRowCellsOwned, row)
		}
		return
	}
	beforeRows := cloneAndPadProtocolRowsRect(snapshot.Screen.Cells, rect.Y, rect.Height, cols)
	beforeTimes := append([]time.Time(nil), snapshot.ScreenTimestamps[rect.Y:rect.Y+rect.Height]...)
	beforeKinds := append([]string(nil), snapshot.ScreenRowKinds[rect.Y:rect.Y+rect.Height]...)
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		ensureSnapshotScreenRowCellsCOW(snapshot, row, screenCellsOwned, screenRowCellsOwned)
		dense := padProtocolCellRow(snapshot.Screen.Cells[row], cols)
		for col := maxInt(rect.X, 0); col < minInt(rect.X+rect.Width, cols); col++ {
			srcX := col - op.Dx
			srcY := row - op.Dy
			if srcX >= rect.X && srcX < rect.X+rect.Width && srcY >= rect.Y && srcY < rect.Y+rect.Height {
				dense[col] = beforeRows[srcY-rect.Y][srcX]
				continue
			}
			dense[col] = protocolBlankCell()
		}
		snapshot.Screen.Cells[row] = trimProtocolCellRow(dense)
	}
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		srcY := row - op.Dy
		if srcY >= rect.Y && srcY < rect.Y+rect.Height {
			snapshot.ScreenTimestamps[row] = beforeTimes[srcY-rect.Y]
			snapshot.ScreenRowKinds[row] = beforeKinds[srcY-rect.Y]
			continue
		}
		snapshot.ScreenTimestamps[row] = time.Time{}
		snapshot.ScreenRowKinds[row] = ""
	}
}

func applySnapshotCopyRect(snapshot *protocol.Snapshot, op protocol.ScreenOp, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned *bool, screenRowCellsOwned map[int]bool) {
	src := op.Src
	if snapshot == nil || src.Width <= 0 || src.Height <= 0 || src.Y < 0 || op.DstY < 0 {
		return
	}
	rowsNeeded := maxInt(src.Y+src.Height, op.DstY+src.Height)
	ensureSnapshotScreenRowsCOW(snapshot, rowsNeeded, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned)
	cols := snapshotScreenWidth(snapshot, maxInt(src.X+src.Width, op.DstX+src.Width))
	fullWidth := src.X == 0 && op.DstX == 0 && src.Width >= cols
	if fullWidth {
		beforeRows := cloneProtocolRowsRect(snapshot.Screen.Cells, src.Y, src.Height)
		beforeTimes := append([]time.Time(nil), snapshot.ScreenTimestamps[src.Y:src.Y+src.Height]...)
		beforeKinds := append([]string(nil), snapshot.ScreenRowKinds[src.Y:src.Y+src.Height]...)
		for row := 0; row < src.Height; row++ {
			dstRow := op.DstY + row
			if dstRow < 0 || dstRow >= len(snapshot.Screen.Cells) {
				continue
			}
			snapshot.Screen.Cells[dstRow] = beforeRows[row]
			snapshot.ScreenTimestamps[dstRow] = beforeTimes[row]
			snapshot.ScreenRowKinds[dstRow] = beforeKinds[row]
			markSnapshotScreenRowOwned(screenRowCellsOwned, dstRow)
		}
		return
	}
	beforeRows := cloneAndPadProtocolRowsRect(snapshot.Screen.Cells, src.Y, src.Height, cols)
	for row := 0; row < src.Height; row++ {
		dstRow := op.DstY + row
		if dstRow < 0 || dstRow >= len(snapshot.Screen.Cells) {
			continue
		}
		ensureSnapshotScreenRowCellsCOW(snapshot, dstRow, screenCellsOwned, screenRowCellsOwned)
		dense := padProtocolCellRow(snapshot.Screen.Cells[dstRow], cols)
		for col := 0; col < src.Width; col++ {
			dstCol := op.DstX + col
			srcCol := src.X + col
			if dstCol < 0 || dstCol >= cols || srcCol < 0 || srcCol >= cols || row >= len(beforeRows) {
				continue
			}
			dense[dstCol] = beforeRows[row][srcCol]
		}
		snapshot.Screen.Cells[dstRow] = trimProtocolCellRow(dense)
	}
}

func padProtocolCellRow(row []protocol.Cell, cols int) []protocol.Cell {
	if cols <= len(row) {
		return row
	}
	if row == nil {
		row = make([]protocol.Cell, 0, cols)
	}
	for len(row) < cols {
		row = append(row, protocolBlankCell())
	}
	return row
}

func protocolBlankCell() protocol.Cell {
	return protocol.Cell{Content: " ", Width: 1}
}

func cloneProtocolRowsRect(rows [][]protocol.Cell, start, height int) [][]protocol.Cell {
	if height <= 0 {
		return nil
	}
	out := make([][]protocol.Cell, height)
	for i := 0; i < height; i++ {
		row := start + i
		if row < 0 || row >= len(rows) {
			continue
		}
		out[i] = cloneProtocolCellRow(rows[row])
	}
	return out
}

func cloneAndPadProtocolRowsRect(rows [][]protocol.Cell, start, height, cols int) [][]protocol.Cell {
	if height <= 0 {
		return nil
	}
	out := make([][]protocol.Cell, height)
	for i := 0; i < height; i++ {
		row := start + i
		if row < 0 || row >= len(rows) {
			out[i] = make([]protocol.Cell, cols)
			for j := range out[i] {
				out[i][j] = protocolBlankCell()
			}
			continue
		}
		out[i] = padProtocolCellRow(cloneProtocolCellRow(rows[row]), cols)
	}
	return out
}

func markSnapshotScreenRowOwned(ownedRows map[int]bool, row int) {
	if ownedRows == nil {
		return
	}
	ownedRows[row] = true
}

func snapshotScreenWidth(snapshot *protocol.Snapshot, minWidth int) int {
	width := minWidth
	if snapshot != nil && int(snapshot.Size.Cols) > width {
		width = int(snapshot.Size.Cols)
	}
	if snapshot != nil {
		for _, row := range snapshot.Screen.Cells {
			if len(row) > width {
				width = len(row)
			}
		}
	}
	if width < 1 {
		return 1
	}
	return width
}

func cowProtocolRows(rows [][]protocol.Cell, size int, owned *bool) [][]protocol.Cell {
	size = maxInt(size, len(rows))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(rows) >= size {
			return rows
		}
		return append(rows, make([][]protocol.Cell, size-len(rows))...)
	}
	out := make([][]protocol.Cell, size)
	copy(out, rows)
	if owned != nil {
		*owned = true
	}
	return out
}

func cowTimeSlice(values []time.Time, size int, owned *bool) []time.Time {
	size = maxInt(size, len(values))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(values) >= size {
			return values
		}
		return append(values, make([]time.Time, size-len(values))...)
	}
	out := make([]time.Time, size)
	copy(out, values)
	if owned != nil {
		*owned = true
	}
	return out
}

func cowStringSlice(values []string, size int, owned *bool) []string {
	size = maxInt(size, len(values))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(values) >= size {
			return values
		}
		return append(values, make([]string, size-len(values))...)
	}
	out := make([]string, size)
	copy(out, values)
	if owned != nil {
		*owned = true
	}
	return out
}

func ensureSnapshotScreenRowsCOW(snapshot *protocol.Snapshot, rows int, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned *bool) {
	if snapshot == nil || rows <= 0 {
		return
	}
	snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, rows, screenCellsOwned)
	snapshot.ScreenTimestamps = cowTimeSlice(snapshot.ScreenTimestamps, rows, screenTimestampsOwned)
	snapshot.ScreenRowKinds = cowStringSlice(snapshot.ScreenRowKinds, rows, screenRowKindsOwned)
	if snapshot.Size.Rows < uint16(rows) {
		snapshot.Size.Rows = uint16(rows)
	}
}

func shiftSnapshotScreenRows(snapshot *protocol.Snapshot, delta int, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned *bool) {
	if snapshot == nil || delta == 0 {
		return
	}
	rows := len(snapshot.Screen.Cells)
	if rows == 0 {
		return
	}
	if delta >= rows || delta <= -rows {
		snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, rows, screenCellsOwned)
		snapshot.ScreenTimestamps = cowTimeSlice(snapshot.ScreenTimestamps, rows, screenTimestampsOwned)
		snapshot.ScreenRowKinds = cowStringSlice(snapshot.ScreenRowKinds, rows, screenRowKindsOwned)
		clear(snapshot.Screen.Cells)
		clear(snapshot.ScreenTimestamps)
		clear(snapshot.ScreenRowKinds)
		return
	}
	snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, rows, screenCellsOwned)
	snapshot.ScreenTimestamps = cowTimeSlice(snapshot.ScreenTimestamps, rows, screenTimestampsOwned)
	snapshot.ScreenRowKinds = cowStringSlice(snapshot.ScreenRowKinds, rows, screenRowKindsOwned)
	if delta > 0 {
		for row := 0; row < rows-delta; row++ {
			snapshot.Screen.Cells[row] = snapshot.Screen.Cells[row+delta]
			snapshot.ScreenTimestamps[row] = snapshot.ScreenTimestamps[row+delta]
			snapshot.ScreenRowKinds[row] = snapshot.ScreenRowKinds[row+delta]
		}
		for row := rows - delta; row < rows; row++ {
			snapshot.Screen.Cells[row] = nil
			snapshot.ScreenTimestamps[row] = time.Time{}
			snapshot.ScreenRowKinds[row] = ""
		}
		return
	}
	shift := -delta
	for row := rows - 1; row >= shift; row-- {
		snapshot.Screen.Cells[row] = snapshot.Screen.Cells[row-shift]
		snapshot.ScreenTimestamps[row] = snapshot.ScreenTimestamps[row-shift]
		snapshot.ScreenRowKinds[row] = snapshot.ScreenRowKinds[row-shift]
	}
	for row := 0; row < shift; row++ {
		snapshot.Screen.Cells[row] = nil
		snapshot.ScreenTimestamps[row] = time.Time{}
		snapshot.ScreenRowKinds[row] = ""
	}
}

func trimSnapshotScrollbackFront(snapshot *protocol.Snapshot, trim int) {
	if snapshot == nil || trim <= 0 {
		return
	}
	if trim >= len(snapshot.Scrollback) {
		snapshot.Scrollback = nil
		snapshot.ScrollbackTimestamps = nil
		snapshot.ScrollbackRowKinds = nil
		return
	}
	snapshot.Scrollback = cloneProtocolRowsWindow(snapshot.Scrollback, trim)
	snapshot.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps[minInt(trim, len(snapshot.ScrollbackTimestamps)):]...)
	snapshot.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds[minInt(trim, len(snapshot.ScrollbackRowKinds)):]...)
}

func cloneProtocolRowsWindow(rows [][]protocol.Cell, start int) [][]protocol.Cell {
	start = minInt(maxInt(start, 0), len(rows))
	if start >= len(rows) {
		return nil
	}
	out := make([][]protocol.Cell, len(rows)-start)
	copy(out, rows[start:])
	return out
}

func maxChangedScreenRow(update protocol.ScreenUpdate) int {
	maxRow := -1
	for _, span := range update.ChangedSpans {
		if span.Row > maxRow {
			maxRow = span.Row
		}
	}
	for _, op := range update.Ops {
		switch op.Code {
		case protocol.ScreenOpWriteSpan, protocol.ScreenOpClearToEOL:
			if op.Row > maxRow {
				maxRow = op.Row
			}
		case protocol.ScreenOpScrollRect, protocol.ScreenOpClearRect:
			if row := op.Rect.Y + op.Rect.Height - 1; row > maxRow {
				maxRow = row
			}
		case protocol.ScreenOpCopyRect:
			if row := op.DstY + op.Src.Height - 1; row > maxRow {
				maxRow = row
			}
		case protocol.ScreenOpResize:
			if row := int(op.Size.Rows) - 1; row > maxRow {
				maxRow = row
			}
		}
	}
	if update.FullReplace && len(update.Screen.Cells) > 0 {
		maxRow = len(update.Screen.Cells) - 1
	}
	return maxRow
}

func maxUint16Local(a, b uint16) uint16 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
