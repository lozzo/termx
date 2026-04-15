package app

import (
	"testing"
	"time"
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
