package render

import (
	"testing"

	"github.com/lozzow/termx/protocol"
)

func TestTerminalExtentProfileForSourceSeparatesVisibleAndOverflowMetrics(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 40, Rows: 10},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: "h", Width: 1}, {Content: "i", Width: 1}},
		}},
	}

	profile := terminalExtentProfileForSource(renderSource(snapshot, nil))
	if profile.Metrics.Cols != 2 || profile.Metrics.Rows != 1 {
		t.Fatalf("expected visible metrics to track rendered bounds, got %#v", profile.Metrics)
	}
	if profile.Overflow.Cols != 40 || profile.Overflow.Rows != 10 {
		t.Fatalf("expected overflow metrics to keep declared terminal extent, got %#v", profile.Overflow)
	}
}

func TestTerminalExtentProfileCachedUsesSnapshotPath(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 12, Rows: 4},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: "x", Width: 1}},
			{{Content: "y", Width: 1}},
		}},
	}

	profile := terminalExtentProfileCached(snapshot, nil, 0)
	if profile.Metrics.Cols != 1 || profile.Metrics.Rows != 2 {
		t.Fatalf("expected cached visible metrics from snapshot rows, got %#v", profile.Metrics)
	}
	if profile.Overflow.Cols != 12 || profile.Overflow.Rows != 4 {
		t.Fatalf("expected cached overflow metrics to preserve declared size, got %#v", profile.Overflow)
	}
}

func TestTerminalExtentProfileForAlternateScreenUsesDeclaredSize(t *testing.T) {
	snapshot := &protocol.Snapshot{
		Size: protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{
			IsAlternateScreen: true,
			Cells: [][]protocol.Cell{
				{{Content: "x", Width: 1}},
			},
		},
		Modes: protocol.TerminalModes{AlternateScreen: true},
	}

	profile := terminalExtentProfileForSource(renderSource(snapshot, nil))
	if profile.Metrics.Cols != 80 || profile.Metrics.Rows != 1 {
		t.Fatalf("expected alternate-screen metrics to keep declared width and visible rows, got %#v", profile.Metrics)
	}
	if profile.Overflow.Cols != 80 || profile.Overflow.Rows != 24 {
		t.Fatalf("expected alternate-screen overflow metrics to preserve declared size, got %#v", profile.Overflow)
	}
}
