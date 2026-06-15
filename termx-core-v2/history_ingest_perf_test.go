package termxcorev2

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkHistoryPipelineIngestPlainLogBatch(b *testing.B) {
	output := benchmarkPlainHistoryOutput(512)
	b.ReportAllocs()
	b.SetBytes(int64(len(output)))

	for i := 0; i < b.N; i++ {
		pipeline := newTerminalHistoryPipeline(80, 24)
		if err := pipeline.Ingest(output); err != nil {
			b.Fatalf("ingest plain output: %v", err)
		}
	}
}

func BenchmarkHistoryPipelineIngestLineEditBatch(b *testing.B) {
	output := benchmarkLineEditHistoryOutput(512)
	b.ReportAllocs()
	b.SetBytes(int64(len(output)))

	for i := 0; i < b.N; i++ {
		pipeline := newTerminalHistoryPipeline(80, 24)
		if err := pipeline.Ingest(output); err != nil {
			b.Fatalf("ingest line-edit output: %v", err)
		}
	}
}

func benchmarkPlainHistoryOutput(lines int) string {
	var builder strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&builder, "%06d [INFO ] worker=%03d status=ok path=/tmp/termx/perf/%06d bytes=%d\n", i, i%128, i, 4096+i)
	}
	return builder.String()
}

func benchmarkLineEditHistoryOutput(lines int) string {
	const suggestion = "-suggest"
	var builder strings.Builder
	for i := 0; i < lines; i++ {
		// 中文说明：模拟 shell autosuggestion 先写灰色建议，再退回光标并清掉临时内容。
		fmt.Fprintf(&builder, "cmd%04d\x1b[90m%s\x1b[0m\x1b[%dD\x1b[K\n", i, suggestion, len(suggestion))
	}
	return builder.String()
}
