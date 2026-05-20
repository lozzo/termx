package app

import (
	"fmt"
	"hash/fnv"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/termx-shared/gridtrace"
)

func traceAppFrameLines(event string, lines []string, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	rows := len(lines)
	bytes := 0
	blockRows := 0
	blockCount := 0
	uriRows := 0
	for row, line := range lines {
		bytes += len(line)
		plain := xansi.Strip(line)
		rowBlocks := appTraceBlockGlyphCount(plain)
		if rowBlocks > 0 {
			blockRows++
			blockCount += rowBlocks
		}
		if strings.Contains(plain, "uri:") || strings.Contains(plain, "expires_at") {
			uriRows++
		}
		if appTraceTextShouldLog(plain) || strings.ContainsAny(line, "█▀▄▌▐") {
			gridtrace.LogLimited(event+".row", 4000, append(kv,
				"row", row,
				"bytes", len(line),
				"plain_bytes", len(plain),
				"plain_cols", xansi.StringWidth(plain),
				"blocks", rowBlocks,
				"hash", appTraceHash(line),
				"plain_hash", appTraceHash(plain),
				"raw_text", gridtrace.Short(line, 220),
				"plain_text", gridtrace.Short(plain, 220),
			)...)
		}
	}
	gridtrace.LogLimited(event+".summary", 1200, append(kv,
		"rows", rows,
		"bytes", bytes,
		"block_rows", blockRows,
		"blocks", blockCount,
		"uri_rows", uriRows,
	)...)
}

func traceAppPayload(event string, payload string, kv ...any) {
	if !gridtrace.Enabled() {
		return
	}
	plain := xansi.Strip(payload)
	blockCount := appTraceBlockGlyphCount(plain)
	interesting := appTraceTextShouldLog(plain) || strings.ContainsAny(payload, "█▀▄▌▐") || blockCount > 0
	gridtrace.LogLimited(event+".summary", 1200, append(kv,
		"bytes", len(payload),
		"plain_bytes", len(plain),
		"plain_cols", xansi.StringWidth(plain),
		"blocks", blockCount,
		"has_uri", strings.Contains(plain, "uri:"),
		"hash", appTraceHash(payload),
		"plain_hash", appTraceHash(plain),
	)...)
	if !interesting {
		return
	}
	gridtrace.LogLimited(event+".sample", 2000, append(kv,
		"bytes", len(payload),
		"plain_bytes", len(plain),
		"blocks", blockCount,
		"raw_text", gridtrace.Short(payload, 300),
		"plain_text", gridtrace.Short(plain, 300),
	)...)
}

func appTraceTextShouldLog(text string) bool {
	return strings.ContainsAny(text, "█▀▄▌▐") ||
		strings.Contains(text, "QR") ||
		strings.Contains(text, "TERMX") ||
		strings.Contains(text, "PROMPT") ||
		strings.Contains(text, "remote pair") ||
		strings.Contains(text, "uri:") ||
		strings.Contains(text, "expires_at") ||
		strings.Contains(text, "000100") ||
		strings.Contains(text, "001000")
}

func appTraceBlockGlyphCount(text string) int {
	count := 0
	for _, r := range text {
		switch r {
		case '█', '▀', '▄', '▌', '▐':
			count++
		}
	}
	return count
}

func appTraceHash(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%016x", h.Sum64())
}
