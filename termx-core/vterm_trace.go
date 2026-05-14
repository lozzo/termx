package termx

import (
	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

func init() {
	vterm.SetTraceHooks(vterm.TraceHooks{
		Measure: perftrace.Measure,
		Count:   perftrace.Count,
	})
}
