package render

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
)

type terminalExtentProfile struct {
	Metrics  renderTerminalMetrics
	Overflow renderTerminalMetrics
}

type terminalSurfaceMetricsCacheKey struct {
	surfaceIdentity string
	surfaceVersion  uint64
}

type terminalExtentMetricsCache struct {
	snapshot sync.Map
	surface  sync.Map
}

var terminalExtentCache terminalExtentMetricsCache

func terminalVisibleMetricsForSource(source terminalRenderSource) renderTerminalMetrics {
	if source == nil {
		return renderTerminalMetrics{}
	}
	metrics := renderTerminalMetrics{
		Cols: int(source.Size().Cols),
		Rows: int(source.Size().Rows),
	}
	renderedRows := source.ScreenRows()
	if renderedRows > 0 && (metrics.Rows <= 0 || renderedRows < metrics.Rows) {
		metrics.Rows = renderedRows
	}
	renderedCols := 0
	for row := source.ScrollbackRows(); row < source.TotalRows(); row++ {
		if rowW := protocolRowDisplayWidth(source.Row(row)); rowW > renderedCols {
			renderedCols = rowW
		}
	}
	if renderedCols > 0 && (metrics.Cols <= 0 || renderedCols < metrics.Cols) {
		metrics.Cols = renderedCols
	}
	return metrics
}

func terminalExtentProfileForSource(source terminalRenderSource) terminalExtentProfile {
	metrics := terminalVisibleMetricsForSource(source)
	return terminalExtentProfile{
		Metrics:  metrics,
		Overflow: terminalOverflowMetricsFromMetrics(metrics, source),
	}
}

func terminalOverflowMetricsForSource(source terminalRenderSource) renderTerminalMetrics {
	return terminalExtentProfileForSource(source).Overflow
}

func terminalOverflowMetricsFromMetrics(metrics renderTerminalMetrics, source terminalRenderSource) renderTerminalMetrics {
	if source == nil {
		return metrics
	}
	size := source.Size()
	if cols := int(size.Cols); cols > metrics.Cols {
		metrics.Cols = cols
	}
	if rows := int(size.Rows); rows > metrics.Rows {
		metrics.Rows = rows
	}
	return metrics
}

func terminalExtentProfileCached(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, surfaceVersion uint64) terminalExtentProfile {
	switch {
	case surface != nil && surfaceVersion != 0:
		identity := terminalSurfaceIdentity(surface)
		if identity == "" {
			break
		}
		key := terminalSurfaceMetricsCacheKey{
			surfaceIdentity: identity,
			surfaceVersion:  surfaceVersion,
		}
		if cached, ok := terminalExtentCache.surface.Load(key); ok {
			return cached.(terminalExtentProfile)
		}
		profile := terminalExtentProfileForSource(renderSource(nil, surface))
		terminalExtentCache.surface.Store(key, profile)
		return profile
	case snapshot != nil:
		if cached, ok := terminalExtentCache.snapshot.Load(snapshot); ok {
			return cached.(terminalExtentProfile)
		}
		profile := terminalExtentProfileForSource(renderSource(snapshot, nil))
		terminalExtentCache.snapshot.Store(snapshot, profile)
		return profile
	default:
		return terminalExtentProfileForSource(renderSource(snapshot, surface))
	}
	return terminalExtentProfileForSource(renderSource(snapshot, surface))
}

func terminalSurfaceIdentity(surface runtime.TerminalSurface) string {
	if surface == nil {
		return ""
	}
	value := reflect.ValueOf(surface)
	switch value.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return fmt.Sprintf("%T:%x", surface, value.Pointer())
	default:
		return fmt.Sprintf("%T:%v", surface, surface)
	}
}
