package termx

import (
	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-vterm/vterm"
)

func init() {
	vterm.SetTraceHooks(vterm.TraceHooks{
		Measure: perftrace.Measure,
		Count:   perftrace.Count,
	})
}
