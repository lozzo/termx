package app

import (
	"bytes"
	"image/color"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-shared/perftrace"
)

const (
	uvFallbackReasonHostWidthSafety  = "host_width_safety"
	uvFallbackReasonHostWidthBackoff = "host_width_backoff"
	uvFallbackReasonSlowBackoff      = "slow_backoff"
	uvFallbackReasonWidthUnavailable = "width_unavailable"
	uvFallbackReasonRenderFailed     = "render_failed"

	uvHostWidthFallbackBackoffAfter  = 4
	uvHostWidthFallbackBackoffFrames = 120
	uvSlowBackoffAfter               = 3
	uvSlowBackoffFrames              = 120
	uvUnsafeRowLogLimit              = 4
	uvUnsafeRowSampleLimit           = 160

	uvSlowFrameThreshold = 8 * time.Millisecond
)

type uvTerminalFrameRenderer struct {
	output   bytes.Buffer
	renderer *uv.TerminalRenderer
	width    int
	height   int
	lines    []string
}

type uvFrameBuildStats struct {
	InputBytes          int
	Rows                int
	Cells               int
	StyledCells         int
	WideCells           int
	EraseCells          int
	HostWidthSafetyRows int
	ClippedCells        int
	TouchedRows         int
	OutputBytes         int
	Resized             bool
	Reused              bool
	BuildMs             float64
	RenderMs            float64
	FlushMs             float64
}

func globalUVRendererEnabled(defaultEnabled bool) bool {
	raw := strings.TrimSpace(os.Getenv("TERMX_EXPERIMENTAL_UV_RENDERER"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("TERMX_GLOBAL_RENDERER"))
	}
	switch strings.ToLower(raw) {
	case "":
		return defaultEnabled
	case "1", "true", "yes", "on", "uv", "ultraviolet":
		return true
	case "0", "false", "no", "off", "default", "legacy", "presenter":
		return false
	default:
		return defaultEnabled
	}
}

func newUVTerminalFrameRenderer() *uvTerminalFrameRenderer {
	r := &uvTerminalFrameRenderer{}
	r.renderer = uv.NewTerminalRenderer(&r.output, uvRendererEnv())
	r.renderer.SetColorProfile(colorprofile.TrueColor)
	r.renderer.SetFullscreen(true)
	r.renderer.SetScrollOptim(true)
	r.renderer.SetRelativeCursor(false)
	r.renderer.Erase()
	perftrace.Count("cursor_writer.renderer.uv.scroll_enabled", 0)
	perftrace.Count("cursor_writer.renderer.uv.truecolor", 0)
	return r
}

func uvRendererEnv() []string {
	env := os.Environ()
	if os.Getenv("TERM") == "" {
		env = append(env, "TERM=xterm-256color")
	}
	return env
}

func (w *outputCursorWriter) resetUVRendererLocked() {
	if w == nil || w.uvRenderer == nil {
		return
	}
	w.uvRenderer = nil
	perftrace.Count("cursor_writer.renderer.uv.reset", 0)
	w.debugLog("cursor_writer.uv.reset")
}

func (w *outputCursorWriter) resetUVFallbackStateLocked() {
	if w == nil {
		return
	}
	w.uvHostWidthFallbacks = 0
	w.uvHostWidthBackoff = 0
	w.uvSlowFrames = 0
	w.uvSlowBackoff = 0
}

func (w *outputCursorWriter) presentFrameLinesWithUVLocked(lines []string, meta *presentMeta) (string, bool, string) {
	if w == nil || len(lines) == 0 {
		return "", false, ""
	}
	if w.uvSlowBackoff > 0 {
		w.uvSlowBackoff--
		perftrace.Count("cursor_writer.renderer.uv.fallback.slow_backoff", w.uvSlowBackoff)
		w.debugLog(
			"cursor_writer.uv.fallback",
			"reason", uvFallbackReasonSlowBackoff,
			"rows", len(lines),
			"remaining", w.uvSlowBackoff,
			"consecutive_slow_frames", w.uvSlowFrames,
		)
		return "", false, uvFallbackReasonSlowBackoff
	}
	if w.uvHostWidthBackoff > 0 {
		w.uvHostWidthBackoff--
		perftrace.Count("cursor_writer.renderer.uv.fallback.host_width_backoff", w.uvHostWidthBackoff)
		w.debugLog(
			"cursor_writer.uv.fallback",
			"reason", uvFallbackReasonHostWidthBackoff,
			"rows", len(lines),
			"remaining", w.uvHostWidthBackoff,
			"consecutive_host_width_fallbacks", w.uvHostWidthFallbacks,
		)
		return "", false, uvFallbackReasonHostWidthBackoff
	}
	width := w.uvFrameWidthLocked(lines, meta)
	if width <= 0 {
		perftrace.Count("cursor_writer.renderer.uv.width_unavailable", len(lines))
		w.debugLog("cursor_writer.uv.fallback", "reason", uvFallbackReasonWidthUnavailable, "rows", len(lines))
		return "", false, uvFallbackReasonWidthUnavailable
	}
	if w.uvRenderer != nil && w.uvRenderer.frameUnchanged(lines, width) {
		w.uvHostWidthFallbacks = 0
		stats := uvFrameBuildStats{
			InputBytes:  joinedLinesLen(lines),
			Rows:        len(lines),
			OutputBytes: 0,
			TouchedRows: 0,
			Reused:      true,
		}
		perftrace.Count("cursor_writer.present.mode.ultraviolet", 0)
		w.debugLogUVFrame(width, len(lines), stats)
		return "", true, ""
	}
	safety := inspectUVFrameHostWidthSafety(lines, uvUnsafeRowLogLimit)
	if safety.UnsafeRows > 0 {
		w.uvHostWidthFallbacks++
		if w.uvHostWidthFallbacks >= uvHostWidthFallbackBackoffAfter {
			w.uvHostWidthBackoff = uvHostWidthFallbackBackoffFrames
		}
		perftrace.Count("cursor_writer.renderer.uv.fallback.host_width_safety", safety.UnsafeRows)
		w.debugLog(
			"cursor_writer.uv.fallback",
			"reason", uvFallbackReasonHostWidthSafety,
			"rows", len(lines),
			"unsafe_rows", safety.UnsafeRows,
			"host_width_rows", safety.HostWidthRows,
			"hidden_emoji_rows", safety.HiddenEmojiRows,
			"sample_count", len(safety.Samples),
			"samples", strings.Join(safety.Samples, " | "),
			"consecutive_host_width_fallbacks", w.uvHostWidthFallbacks,
			"next_backoff", w.uvHostWidthBackoff,
		)
		return "", false, uvFallbackReasonHostWidthSafety
	}
	w.uvHostWidthFallbacks = 0
	w.uvHostWidthBackoff = 0
	if w.uvRenderer == nil {
		w.uvRenderer = newUVTerminalFrameRenderer()
		w.debugLog("cursor_writer.uv.init", "term", os.Getenv("TERM"), "scroll_optim", true, "color_profile", "truecolor")
	}
	payload, stats, ok := w.uvRenderer.render(lines, width)
	if !ok {
		w.debugLog("cursor_writer.uv.fallback", "reason", uvFallbackReasonRenderFailed, "rows", len(lines), "width", width)
		return "", false, uvFallbackReasonRenderFailed
	}
	traceAppPayload("app.frame.uv.render_payload", payload, "rows", len(lines), "width", width, "cells", stats.Cells, "touched_rows", stats.TouchedRows)
	perftrace.Count("cursor_writer.present.mode.ultraviolet", stats.OutputBytes)
	w.debugLogUVFrame(width, len(lines), stats)
	return payload, true, ""
}

func (w *outputCursorWriter) debugLogUVFrame(width, height int, stats uvFrameBuildStats) {
	if w == nil {
		return
	}
	w.debugLog(
		"cursor_writer.uv.frame",
		"width", width,
		"height", height,
		"input_bytes", stats.InputBytes,
		"output_bytes", stats.OutputBytes,
		"cells", stats.Cells,
		"styled_cells", stats.StyledCells,
		"wide_cells", stats.WideCells,
		"erase_cells", stats.EraseCells,
		"host_width_safety_rows", stats.HostWidthSafetyRows,
		"clipped_cells", stats.ClippedCells,
		"touched_rows", stats.TouchedRows,
		"resized", stats.Resized,
		"reused", stats.Reused,
		"build_ms", stats.BuildMs,
		"render_ms", stats.RenderMs,
		"flush_ms", stats.FlushMs,
	)
}

func (w *outputCursorWriter) observeUVSuccessLocked(elapsed time.Duration, rows int, payloadBytes int) {
	if w == nil {
		return
	}
	if elapsed <= uvSlowFrameThreshold {
		if w.uvSlowFrames != 0 {
			w.debugLog("cursor_writer.uv.slow_recovered", "elapsed_ms", float64(elapsed.Microseconds())/1000.0, "previous_slow_frames", w.uvSlowFrames)
		}
		w.uvSlowFrames = 0
		return
	}
	w.uvSlowFrames++
	perftrace.Count("cursor_writer.renderer.uv.slow_frame", rows)
	nextBackoff := 0
	if w.uvSlowFrames >= uvSlowBackoffAfter {
		w.uvSlowBackoff = uvSlowBackoffFrames
		nextBackoff = w.uvSlowBackoff
		perftrace.Count("cursor_writer.renderer.uv.slow_backoff", rows)
	}
	w.debugLog(
		"cursor_writer.uv.slow",
		"elapsed_ms", float64(elapsed.Microseconds())/1000.0,
		"threshold_ms", float64(uvSlowFrameThreshold.Microseconds())/1000.0,
		"rows", rows,
		"payload_bytes", payloadBytes,
		"consecutive_slow_frames", w.uvSlowFrames,
		"next_backoff", nextBackoff,
	)
}

type uvHostWidthSafetyInspection struct {
	UnsafeRows      int
	HostWidthRows   int
	HiddenEmojiRows int
	Samples         []string
}

func inspectUVFrameHostWidthSafety(lines []string, sampleLimit int) uvHostWidthSafetyInspection {
	out := uvHostWidthSafetyInspection{}
	if sampleLimit < 0 {
		sampleLimit = 0
	}
	for y, line := range lines {
		row := parsePresentedRow(line)
		if row.hasHostWidthStabilizer || row.hasHiddenEmojiCompensation {
			out.UnsafeRows++
			if row.hasHostWidthStabilizer {
				out.HostWidthRows++
			}
			if row.hasHiddenEmojiCompensation {
				out.HiddenEmojiRows++
			}
			if len(out.Samples) < sampleLimit {
				out.Samples = append(out.Samples, uvUnsafeRowSample(y, row))
			}
		}
		releasePresentedCells(row.cells)
	}
	return out
}

func uvUnsafeRowSample(rowIndex int, row presentedRow) string {
	reanchorCells := 0
	eraseCells := 0
	wideCells := 0
	cellWidth := 0
	for _, cell := range row.cells {
		if cell.ReanchorBefore {
			reanchorCells++
		}
		if cell.Erase {
			eraseCells++
		}
		if cell.Width > 1 {
			wideCells++
		}
		cellWidth += maxInt(1, cell.Width)
	}
	return strings.Join([]string{
		"row=" + strconv.Itoa(rowIndex),
		"host_width=" + strconv.FormatBool(row.hasHostWidthStabilizer),
		"hidden_emoji=" + strconv.FormatBool(row.hasHiddenEmojiCompensation),
		"cells=" + strconv.Itoa(len(row.cells)),
		"cell_width=" + strconv.Itoa(cellWidth),
		"wide_cells=" + strconv.Itoa(wideCells),
		"erase_cells=" + strconv.Itoa(eraseCells),
		"reanchor_cells=" + strconv.Itoa(reanchorCells),
		"raw=" + strconv.Quote(truncateDebugSample(row.raw, uvUnsafeRowSampleLimit)),
	}, ",")
}

func truncateDebugSample(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= len("...") {
		return value[:limit]
	}
	end := limit - len("...")
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	if end <= 0 {
		return "..."
	}
	return value[:end] + "..."
}

func (w *outputCursorWriter) uvFrameWidthLocked(lines []string, meta *presentMeta) int {
	if w != nil && w.ttyWidth > 0 {
		return w.ttyWidth
	}
	if meta != nil && meta.Width > 0 {
		return meta.Width
	}
	width := 0
	for _, line := range lines {
		row := parsePresentedRow(line)
		width = maxInt(width, presentedRowCellWidth(row))
		releasePresentedCells(row.cells)
	}
	return width
}

func (r *uvTerminalFrameRenderer) render(lines []string, width int) (string, uvFrameBuildStats, bool) {
	stats := uvFrameBuildStats{Rows: len(lines), InputBytes: joinedLinesLen(lines)}
	if r == nil || r.renderer == nil || width <= 0 || len(lines) == 0 {
		return "", stats, false
	}

	buildFinish := perftrace.Measure("cursor_writer.renderer.uv.buffer_build")
	buildStart := time.Now()
	buf, buildStats, ok := buildUVRenderBufferFromLines(lines, width)
	buildFinish(buildStats.InputBytes)
	stats = buildStats
	stats.BuildMs = float64(time.Since(buildStart).Microseconds()) / 1000.0
	if !ok {
		return "", stats, false
	}

	height := len(lines)
	fullRender := r.width != width || r.height != height || len(r.lines) != height
	stats.TouchedRows = markUVRenderBufferTouchedRows(buf, r.lines, lines, fullRender)
	if r.width != width || r.height != height {
		r.renderer.Resize(width, height)
		r.renderer.Erase()
		r.width = width
		r.height = height
		stats.Resized = true
		perftrace.Count("cursor_writer.renderer.uv.resize", width*height)
	}

	r.output.Reset()
	r.renderer.SetPosition(0, 0)
	renderFinish := perftrace.Measure("cursor_writer.renderer.uv.render")
	renderStart := time.Now()
	r.renderer.Render(buf)
	renderFinish(r.renderer.Buffered())
	stats.RenderMs = float64(time.Since(renderStart).Microseconds()) / 1000.0

	flushFinish := perftrace.Measure("cursor_writer.renderer.uv.flush")
	flushStart := time.Now()
	err := r.renderer.Flush()
	payload := r.output.String()
	flushFinish(len(payload))
	stats.FlushMs = float64(time.Since(flushStart).Microseconds()) / 1000.0
	if err != nil {
		perftrace.Count("cursor_writer.renderer.uv.flush_error", 0)
		return "", stats, false
	}

	stats.OutputBytes = len(payload)
	r.lines = append(r.lines[:0], lines...)
	perftrace.Count("cursor_writer.renderer.uv.input_bytes", stats.InputBytes)
	perftrace.Count("cursor_writer.renderer.uv.output_bytes", stats.OutputBytes)
	perftrace.Count("cursor_writer.renderer.uv.rows", stats.Rows)
	perftrace.Count("cursor_writer.renderer.uv.cells", stats.Cells)
	perftrace.Count("cursor_writer.renderer.uv.styled_cells", stats.StyledCells)
	perftrace.Count("cursor_writer.renderer.uv.wide_cells", stats.WideCells)
	perftrace.Count("cursor_writer.renderer.uv.erase_cells", stats.EraseCells)
	perftrace.Count("cursor_writer.renderer.uv.host_width_safety_rows", stats.HostWidthSafetyRows)
	perftrace.Count("cursor_writer.renderer.uv.clipped_cells", stats.ClippedCells)
	perftrace.Count("cursor_writer.renderer.uv.touched_rows", stats.TouchedRows)
	return payload, stats, true
}

func (r *uvTerminalFrameRenderer) frameUnchanged(lines []string, width int) bool {
	if r == nil || r.renderer == nil || r.width != width || r.height != len(lines) || len(r.lines) != len(lines) {
		return false
	}
	for i := range lines {
		if r.lines[i] != lines[i] {
			return false
		}
	}
	return true
}

func markUVRenderBufferTouchedRows(buf *uv.RenderBuffer, previous, next []string, full bool) int {
	if buf == nil {
		return 0
	}
	height := len(next)
	if len(buf.Touched) != height {
		buf.Touched = make([]*uv.LineData, height)
	} else {
		clear(buf.Touched)
	}
	width := buf.Width()
	if full || len(previous) != height {
		for row := 0; row < height; row++ {
			buf.Touched[row] = &uv.LineData{FirstCell: 0, LastCell: width}
		}
		return height
	}
	touched := 0
	for row := range next {
		if previous[row] == next[row] {
			continue
		}
		buf.Touched[row] = &uv.LineData{FirstCell: 0, LastCell: width}
		touched++
	}
	return touched
}

func buildUVRenderBufferFromLines(lines []string, width int) (*uv.RenderBuffer, uvFrameBuildStats, bool) {
	stats := uvFrameBuildStats{
		Rows:       len(lines),
		InputBytes: joinedLinesLen(lines),
	}
	if width <= 0 || len(lines) == 0 {
		return nil, stats, false
	}
	buf := uv.NewRenderBuffer(width, len(lines))
	for y, line := range lines {
		row := parsePresentedRow(line)
		if row.hasHostWidthStabilizer || row.hasHiddenEmojiCompensation {
			stats.HostWidthSafetyRows++
		}
		x := 0
		for _, cell := range row.cells {
			cellWidth := maxInt(1, cell.Width)
			if x >= width {
				stats.ClippedCells++
				continue
			}
			if x+cellWidth > width {
				stats.ClippedCells++
				break
			}
			buf.SetCell(x, y, uvCellFromPresentedCell(cell, cellWidth))
			stats.Cells++
			if cell.Style != (presentedStyle{}) {
				stats.StyledCells++
			}
			if cellWidth > 1 {
				stats.WideCells++
			}
			if cell.Erase {
				stats.EraseCells++
			}
			x += cellWidth
		}
		buf.TouchLine(0, y, width)
		releasePresentedCells(row.cells)
	}
	return buf, stats, true
}

func presentedRowCellWidth(row presentedRow) int {
	width := 0
	for _, cell := range row.cells {
		width += maxInt(1, cell.Width)
	}
	return width
}

func uvCellFromPresentedCell(cell presentedCell, width int) *uv.Cell {
	content := cell.Content
	if content == "" {
		content = " "
	}
	return &uv.Cell{
		Content: content,
		Width:   maxInt(1, width),
		Style:   uvStyleFromPresentedStyle(cell.Style),
	}
}

func uvStyleFromPresentedStyle(style presentedStyle) uv.Style {
	out := uv.Style{
		Fg: uvColorFromPresentedSGR(style.FGCode),
		Bg: uvColorFromPresentedSGR(style.BGCode),
	}
	if style.Bold {
		out.Attrs |= uv.AttrBold
	}
	if style.Italic {
		out.Attrs |= uv.AttrItalic
	}
	if style.Blink {
		out.Attrs |= uv.AttrBlink
	}
	if style.Reverse {
		out.Attrs |= uv.AttrReverse
	}
	if style.Strikethrough {
		out.Attrs |= uv.AttrStrikethrough
	}
	if style.Underline {
		out.Underline = uv.UnderlineSingle
	}
	return out
}

func uvColorFromPresentedSGR(code string) color.Color {
	if code == "" {
		return nil
	}
	parts := strings.Split(code, ";")
	if len(parts) == 0 {
		return nil
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}
	switch {
	case first >= 30 && first <= 37:
		return xansi.Black + xansi.BasicColor(first-30)
	case first >= 90 && first <= 97:
		return xansi.BrightBlack + xansi.BasicColor(first-90)
	case first >= 40 && first <= 47:
		return xansi.Black + xansi.BasicColor(first-40)
	case first >= 100 && first <= 107:
		return xansi.BrightBlack + xansi.BasicColor(first-100)
	case (first == 38 || first == 48) && len(parts) >= 3:
		mode, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		switch mode {
		case 5:
			value, err := strconv.Atoi(parts[2])
			if err != nil || value < 0 || value > 255 {
				return nil
			}
			return xansi.IndexedColor(value)
		case 2:
			if len(parts) < 5 {
				return nil
			}
			r, okR := parseSGRColorByte(parts[2])
			g, okG := parseSGRColorByte(parts[3])
			b, okB := parseSGRColorByte(parts[4])
			if !okR || !okG || !okB {
				return nil
			}
			return color.RGBA{R: r, G: g, B: b, A: 255}
		}
	}
	return nil
}

func parseSGRColorByte(raw string) (uint8, bool) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > 255 {
		return 0, false
	}
	return uint8(value), true
}
