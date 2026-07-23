package render

import (
	"strings"
	"testing"

	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/tui/state"
)

func TestConnectionsOverlayProjectsActualRouteGenerationAndPriorityAtNarrowWidths(t *testing.T) {
	zero := 0
	root := state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 80, Rows: 24},
		Shell:    state.DefaultShell().OpenConnections(),
		Endpoints: (state.EndpointStore{}).Upsert(state.EndpointItem{
			ID: "studio", Label: "Studio", Enabled: true, Status: state.EndpointStatusConnected,
			RoutePreference: endpoint.RoutePreferenceAuto, ActiveRouteID: "cloud", ConnectionGeneration: 12,
			ObservedPath: "single_relay", RouteSelectionReason: "first_ready",
			Routes: []state.EndpointRouteItem{
				{ID: "cloud", Kind: state.EndpointTransportHubP2P, Enabled: true, Priority: &zero, AvailabilityKnown: true, Available: true, AvailabilityReason: endpoint.RouteAvailabilityAvailable, RelayMode: endpoint.RelayAuto, RelayTransport: endpoint.RelayTransportTCP},
				{ID: "ssh", Kind: state.EndpointTransportSSH, Enabled: true, Priority: &zero, AvailabilityKnown: true, Available: false, AvailabilityReason: endpoint.RouteAvailabilityCredentialUnavailable},
			},
		}),
	}
	for _, cols := range []int{80, 56} {
		root.Viewport.Cols = cols
		vm := NewRenderVMBuilder().Build(root)
		if vm.Shell.Overlay.Kind != OverlayConnections || vm.Shell.Overlay.Content.Kind != ContentConnections || !vm.Shell.Overlay.Opaque {
			t.Fatalf("connections overlay projection = %#v", vm.Shell.Overlay)
		}
		frame := NewRenderer(DefaultTheme()).Render(vm)
		text := frameText(frame)
		for _, want := range []string{"Connections", "CURRENT CONNECTION", "NEXT CONNECTION POLICY", "cloud", "single_relay", "12", "priority 0", "available", "credential missing"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%d-column frame missing %q:\n%s", cols, want, text)
			}
		}
	}
}

func TestConnectionsEntryIsDiscoverableInNarrowSystemFooterAndHelp(t *testing.T) {
	root := state.Root{
		Viewport: state.ViewportStore{Valid: true, Cols: 56, Rows: 24},
		Shell:    state.DefaultShell().SetInteractionMode(state.InteractionModeGlobal),
	}
	frame := NewRenderer(DefaultTheme()).Render(NewRenderVMBuilder().Build(root))
	if text := frameText(frame); !strings.Contains(text, "CONNECTIONS") {
		t.Fatalf("56-column system footer hides Connections entry:\n%s", text)
	}
	root.Shell = state.DefaultShell().OpenHelp("most-used")
	help := plainLines(NewRenderVMBuilder().Build(root).Shell.Overlay.Content.Lines)
	if !strings.Contains(help, "Connections") || !strings.Contains(help, "e CONNECTIONS") {
		t.Fatalf("help does not expose Connections entry: %q", help)
	}
}

func frameText(frame Frame) string {
	lines := make([]string, 0, len(frame.Lines))
	for _, line := range frame.Lines {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
