package shared

import "testing"

func TestHostPaletteProbeEnabledDefaultsOffInRemoteLatencyMode(t *testing.T) {
	t.Setenv("TERMX_HOST_PALETTE_PROBE", "")
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	if HostPaletteProbeEnabled() {
		t.Fatal("expected host palette probe disabled in remote latency mode")
	}
}

func TestHostPaletteProbeEnabledCanBeForcedOn(t *testing.T) {
	t.Setenv("TERMX_HOST_PALETTE_PROBE", "always")
	t.Setenv("TERMX_REMOTE_LATENCY", "1")
	if !HostPaletteProbeEnabled() {
		t.Fatal("expected host palette probe forced on")
	}
}

func TestBubbleTeaRendererEnabledFollowsEnv(t *testing.T) {
	t.Setenv("TERMX_USE_BUBBLETEA_RENDERER", "1")
	if !BubbleTeaRendererEnabled() {
		t.Fatal("expected Bubble Tea renderer flag enabled")
	}
	t.Setenv("TERMX_USE_BUBBLETEA_RENDERER", "0")
	if BubbleTeaRendererEnabled() {
		t.Fatal("expected Bubble Tea renderer flag disabled")
	}
}
