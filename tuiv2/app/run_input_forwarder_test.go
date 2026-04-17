package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEffectiveInputBurstBatchDelayUsesRemoteProfile(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_INPUT_BURST_BATCH_DELAY", "")

	if got := effectiveInputBurstBatchDelay(); got != remoteInputBurstBatchDelay {
		t.Fatalf("expected remote input burst delay %v, got %v", remoteInputBurstBatchDelay, got)
	}
}

func TestEffectiveInputBurstBatchDelayHonorsEnvOverride(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_INPUT_BURST_BATCH_DELAY", "1750us")

	if got := effectiveInputBurstBatchDelay(); got != 1750*time.Microsecond {
		t.Fatalf("expected env override delay %v, got %v", 1750*time.Microsecond, got)
	}
}

func TestEffectiveMouseMotionBatchDelayUsesRemoteProfile(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_MOUSE_MOTION_BATCH_DELAY", "")

	if got := effectiveMouseMotionBatchDelay(); got != remoteMouseMotionBatchDelay {
		t.Fatalf("expected remote mouse motion delay %v, got %v", remoteMouseMotionBatchDelay, got)
	}
}

func TestEffectiveMouseMotionBatchDelayHonorsEnvOverride(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_MOUSE_MOTION_BATCH_DELAY", "3ms")

	if got := effectiveMouseMotionBatchDelay(); got != 3*time.Millisecond {
		t.Fatalf("expected env override delay %v, got %v", 3*time.Millisecond, got)
	}
}

func TestEffectiveMouseWheelBatchDelayUsesRemoteProfile(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_MOUSE_WHEEL_BATCH_DELAY", "")

	if got := effectiveMouseWheelBatchDelay(); got != remoteMouseWheelBatchDelay {
		t.Fatalf("expected remote mouse wheel delay %v, got %v", remoteMouseWheelBatchDelay, got)
	}
}

func TestEffectiveMouseWheelBatchDelayHonorsEnvOverride(t *testing.T) {
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	t.Setenv("TERMX_MOUSE_WHEEL_BATCH_DELAY", "750us")

	if got := effectiveMouseWheelBatchDelay(); got != 750*time.Microsecond {
		t.Fatalf("expected env override delay %v, got %v", 750*time.Microsecond, got)
	}
}

func TestHighFrequencyMouseAccumulatorKeepsOnlyLatestMotion(t *testing.T) {
	var accum highFrequencyMouseAccumulator
	accum.QueueMotion(queuedMouseMsg{Seq: 1, Kind: "motion", Msg: tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionMotion}})
	accum.QueueMotion(queuedMouseMsg{Seq: 2, Kind: "motion", Msg: tea.MouseMsg{X: 30, Y: 12, Action: tea.MouseActionMotion}})

	msgs := accum.Flush()
	if len(msgs) != 1 {
		t.Fatalf("expected one flushed motion, got %d (%#v)", len(msgs), msgs)
	}
	got, ok := msgs[0].(queuedMouseMsg)
	if !ok {
		t.Fatalf("expected queuedMouseMsg, got %T", msgs[0])
	}
	if got.Seq != 2 || got.Msg.X != 30 || got.Msg.Y != 12 {
		t.Fatalf("expected latest motion preserved, got %#v", got)
	}
}

func TestHighFrequencyMouseAccumulatorAccumulatesWheelRepeat(t *testing.T) {
	var accum highFrequencyMouseAccumulator
	accum.QueueWheel(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 8, Y: 3})
	accum.QueueWheel(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 8, Y: 3})
	accum.QueueWheel(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 8, Y: 3})

	msgs := accum.Flush()
	if len(msgs) != 1 {
		t.Fatalf("expected one flushed wheel burst, got %d (%#v)", len(msgs), msgs)
	}
	got, ok := msgs[0].(mouseWheelBurstMsg)
	if !ok {
		t.Fatalf("expected mouseWheelBurstMsg, got %T", msgs[0])
	}
	if got.Repeat != 3 {
		t.Fatalf("expected wheel repeat 3, got %#v", got)
	}
}

func TestHighFrequencyMouseAccumulatorKeepsWheelAndLatestMotionSeparate(t *testing.T) {
	var accum highFrequencyMouseAccumulator
	accum.QueueWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, X: 8, Y: 3})
	accum.QueueMotion(queuedMouseMsg{Seq: 7, Kind: "motion", Msg: tea.MouseMsg{X: 20, Y: 9, Action: tea.MouseActionMotion}})

	msgs := accum.Flush()
	if len(msgs) != 2 {
		t.Fatalf("expected wheel + motion flush, got %d (%#v)", len(msgs), msgs)
	}
	if _, ok := msgs[0].(queuedMouseMsg); !ok {
		t.Fatalf("expected latest motion first, got %T", msgs[0])
	}
	if _, ok := msgs[1].(mouseWheelBurstMsg); !ok {
		t.Fatalf("expected wheel burst second, got %T", msgs[1])
	}
}

func TestHighFrequencyMouseAccumulatorCancelsOpposingWheelBurst(t *testing.T) {
	var accum highFrequencyMouseAccumulator
	accum.QueueWheel(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 8, Y: 3})
	accum.QueueWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, X: 8, Y: 3})

	msgs := accum.Flush()
	if len(msgs) != 0 {
		t.Fatalf("expected opposing wheel bursts to cancel, got %#v", msgs)
	}
}

func TestHighFrequencyMouseAccumulatorResetClearsPendingContinuousInput(t *testing.T) {
	var accum highFrequencyMouseAccumulator
	accum.QueueMotion(queuedMouseMsg{Seq: 1, Kind: "motion", Msg: tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionMotion}})
	accum.QueueWheel(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 8, Y: 3})
	accum.Reset()
	if msgs := accum.Flush(); len(msgs) != 0 {
		t.Fatalf("expected reset accumulator to drop pending continuous input, got %#v", msgs)
	}
}
