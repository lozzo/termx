package runtime

import (
	"context"
	"fmt"
	"image/color"
	"maps"
	"slices"
	"sort"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/terminalmeta"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/termx-core/vterm"
)

type Runtime struct {
	registry      *TerminalRegistry
	bindings      map[string]*PaneBinding
	client        bridge.Client
	onInvalidate  func()
	onTitleChange func(terminalID, title string)
	newVTerm      VTermFactory
	hostDefaultFG string
	hostDefaultBG string
	hostPalette   map[int]string
	hostEmojiVS16 shared.AmbiguousEmojiVariationSelectorMode

	version        uint64
	visibleVersion uint64
	visibleCache   *VisibleRuntime
	recentInputAt  atomic.Int64
}

const interactiveLatencyWindow = 24 * time.Millisecond
const remoteInteractiveLatencyWindow = 150 * time.Millisecond

func New(client bridge.Client, opts ...Option) *Runtime {
	r := &Runtime{
		registry:      NewTerminalRegistry(),
		bindings:      make(map[string]*PaneBinding),
		client:        client,
		onInvalidate:  func() {},
		onTitleChange: func(string, string) {},
		hostEmojiVS16: shared.AmbiguousEmojiVariationSelectorRaw,
	}
	r.newVTerm = r.defaultVTermFactory
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

func (r *Runtime) Client() bridge.Client {
	if r == nil {
		return nil
	}
	return r.client
}

func (r *Runtime) Registry() *TerminalRegistry {
	if r == nil {
		return nil
	}
	return r.registry
}

func (r *Runtime) Binding(paneID string) *PaneBinding {
	if r == nil {
		return nil
	}
	return r.bindings[paneID]
}

func (r *Runtime) BindPane(paneID string) *PaneBinding {
	if r == nil || paneID == "" {
		return nil
	}
	binding := r.bindings[paneID]
	if binding != nil {
		return binding
	}
	binding = &PaneBinding{PaneID: paneID}
	r.bindings[paneID] = binding
	r.touch()
	return binding
}

func (r *Runtime) UnbindPane(paneID, terminalID string) {
	if r == nil || paneID == "" {
		return
	}
	delete(r.bindings, paneID)
	r.unbindPaneFromTerminalCache(paneID, terminalID)
	r.touch()
}

func (r *Runtime) unbindPaneFromTerminalCache(paneID, terminalID string) {
	if r == nil || r.registry == nil || paneID == "" {
		return
	}
	if terminalID != "" {
		r.clearPaneFromTerminal(r.registry.Get(terminalID), paneID)
		return
	}
	for _, id := range r.registry.IDs() {
		r.clearPaneFromTerminal(r.registry.Get(id), paneID)
	}
}

func (r *Runtime) clearPaneFromTerminal(terminal *TerminalRuntime, paneID string) {
	if terminal == nil || paneID == "" {
		return
	}
	terminal.BoundPaneIDs = removeBoundPaneID(terminal.BoundPaneIDs, paneID)
	if terminal.OwnerPaneID == paneID {
		terminal.OwnerPaneID = ""
		terminal.RequiresExplicitOwner = true
	}
	if terminal.ControlPaneID == paneID {
		terminal.ControlPaneID = ""
	}
	r.syncTerminalOwnership(terminal)
}

func (r *Runtime) touch() {
	if r == nil {
		return
	}
	r.version++
}

func WithInvalidate(fn func()) Option {
	return func(r *Runtime) {
		r.SetInvalidate(fn)
	}
}

func WithVTermFactory(factory VTermFactory) Option {
	return func(r *Runtime) {
		if r == nil || factory == nil {
			return
		}
		r.newVTerm = factory
	}
}

func (r *Runtime) defaultVTermFactory(channel uint16) VTermLike {
	return localvterm.New(80, 24, 10000, func(data []byte) {
		if r == nil || r.client == nil || channel == 0 || len(data) == 0 {
			return
		}
		_ = r.client.Input(context.Background(), channel, data)
	})
}

func (r *Runtime) ensureVTerm(terminal *TerminalRuntime) VTermLike {
	if r == nil || terminal == nil {
		return nil
	}
	if terminal.VTerm == nil {
		if r.newVTerm == nil {
			r.newVTerm = r.defaultVTermFactory
		}
		terminal.VTerm = r.newVTerm(terminal.Channel)
		r.applyHostColorsToVTerm(terminal.VTerm)
		if terminal.Snapshot != nil {
			loadSnapshotIntoVTerm(terminal.VTerm, terminal.Snapshot)
		}
	}
	terminal.VTerm.SetTitleHandler(func(title string) {
		terminal.Title = title
		r.touch()
		if r.onTitleChange != nil {
			r.onTitleChange(terminal.TerminalID, title)
		}
		r.invalidate()
	})
	return terminal.VTerm
}

func (r *Runtime) SetHostDefaultColors(fg, bg color.Color) {
	r.applyHostTheme(fg, bg, nil, true)
}

func (r *Runtime) ApplyHostTheme(fg, bg color.Color, palette map[int]color.Color) {
	r.applyHostTheme(fg, bg, palette, true)
}

func (r *Runtime) ApplyHostThemeSilently(fg, bg color.Color, palette map[int]color.Color) {
	r.applyHostTheme(fg, bg, palette, false)
}

func (r *Runtime) applyHostTheme(fg, bg color.Color, palette map[int]color.Color, invalidate bool) {
	if r == nil {
		return
	}
	changed := false
	nextFG := r.hostDefaultFG
	nextBG := r.hostDefaultBG
	if fg != nil {
		nextFG = colorToHex(fg)
	}
	if bg != nil {
		nextBG = colorToHex(bg)
	}
	if nextFG != r.hostDefaultFG || nextBG != r.hostDefaultBG {
		r.hostDefaultFG = nextFG
		r.hostDefaultBG = nextBG
		changed = true
	}
	var changedPalette map[int]string
	if len(palette) > 0 {
		if r.hostPalette == nil {
			r.hostPalette = make(map[int]string)
		}
		for index, c := range palette {
			if c == nil || index < 0 || index > 255 {
				continue
			}
			value := colorToHex(c)
			if r.hostPalette[index] == value {
				continue
			}
			r.hostPalette[index] = value
			if changedPalette == nil {
				changedPalette = make(map[int]string)
			}
			changedPalette[index] = value
			changed = true
		}
	}
	if !changed {
		return
	}
	for _, terminalID := range r.registry.IDs() {
		terminal := r.registry.Get(terminalID)
		if terminal == nil || terminal.VTerm == nil {
			continue
		}
		terminal.VTerm.SetDefaultColors(r.hostDefaultFG, r.hostDefaultBG)
		for index, value := range changedPalette {
			terminal.VTerm.SetIndexedColor(index, value)
		}
	}
	if invalidate {
		r.invalidate()
		return
	}
	r.touch()
}

func (r *Runtime) SetHostPaletteColor(index int, c color.Color) {
	if r == nil || c == nil || index < 0 || index > 255 {
		return
	}
	r.applyHostTheme(nil, nil, map[int]color.Color{index: c}, true)
}

func (r *Runtime) SetHostAmbiguousEmojiVariationSelectorMode(mode shared.AmbiguousEmojiVariationSelectorMode) {
	if r == nil {
		return
	}
	// Keep the negotiated host behavior in visible runtime state so render can
	// switch serialization strategies without re-probing every pane.
	switch mode {
	case shared.AmbiguousEmojiVariationSelectorRaw, shared.AmbiguousEmojiVariationSelectorAdvance, shared.AmbiguousEmojiVariationSelectorStrip:
	default:
		mode = shared.AmbiguousEmojiVariationSelectorRaw
	}
	if r.hostEmojiVS16 == mode {
		return
	}
	r.hostEmojiVS16 = mode
	r.invalidate()
}

func (r *Runtime) applyHostColorsToVTerm(vt VTermLike) {
	if r == nil || vt == nil {
		return
	}
	vt.SetDefaultColors(r.hostDefaultFG, r.hostDefaultBG)
	for index, value := range r.hostPalette {
		vt.SetIndexedColor(index, value)
	}
}

func colorToHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

func (r *Runtime) invalidate() {
	if r == nil {
		return
	}
	perftrace.Count("runtime.invalidate", 0)
	r.touch()
	if r.onInvalidate == nil {
		return
	}
	r.onInvalidate()
}

func (r *Runtime) SetInvalidate(fn func()) {
	if r == nil || fn == nil {
		return
	}
	r.onInvalidate = fn
}

func (r *Runtime) SetTitleChange(fn func(terminalID, title string)) {
	if r == nil || fn == nil {
		return
	}
	r.onTitleChange = fn
}

func (r *Runtime) ListTerminals(ctx context.Context) ([]protocol.TerminalInfo, error) {
	if r == nil || r.client == nil {
		return nil, shared.UserVisibleError{Op: "list terminals", Err: fmt.Errorf("runtime client is nil")}
	}
	result, err := r.client.List(ctx)
	if err != nil {
		return nil, shared.UserVisibleError{Op: "list terminals", Err: err}
	}
	return append([]protocol.TerminalInfo(nil), result.Terminals...), nil
}

func (r *Runtime) Visible() *VisibleRuntime {
	finish := perftrace.Measure("runtime.visible")
	cacheMetric := "runtime.visible.cache_miss"
	defer func() {
		perftrace.Count(cacheMetric, 0)
		finish(0)
	}()
	if r == nil || r.registry == nil {
		return nil
	}
	if r.visibleCache != nil && r.visibleVersion == r.version {
		cacheMetric = "runtime.visible.cache_hit"
		return r.visibleCache
	}
	visible := &VisibleRuntime{
		Terminals:         make([]VisibleTerminal, 0, len(r.registry.terminals)),
		Bindings:          make([]VisiblePaneBinding, 0, len(r.bindings)),
		HostDefaultFG:     r.hostDefaultFG,
		HostDefaultBG:     r.hostDefaultBG,
		HostPalette:       maps.Clone(r.hostPalette),
		HostEmojiVS16Mode: r.hostEmojiVS16,
	}
	for _, terminalID := range r.registry.IDs() {
		terminal := r.registry.Get(terminalID)
		if terminal == nil {
			continue
		}
		visible.Terminals = append(visible.Terminals, VisibleTerminal{
			TerminalID:      terminal.TerminalID,
			Name:            terminal.Name,
			State:           terminal.State,
			ExitCode:        terminal.ExitCode,
			Title:           terminal.Title,
			AttachMode:      terminal.AttachMode,
			OwnerPaneID:     terminal.OwnerPaneID,
			BoundPaneIDs:    slices.Clone(terminal.BoundPaneIDs),
			SizeLocked:      terminalmeta.SizeLocked(terminal.Tags),
			Snapshot:        terminal.Snapshot,
			Surface:         visibleSurface(terminal),
			SurfaceVersion:  terminal.SurfaceVersion,
			SnapshotVersion: terminal.SnapshotVersion,
			ScreenUpdate: VisibleScreenUpdateSummary{
				SurfaceVersion: terminal.ScreenUpdate.SurfaceVersion,
				FullReplace:    terminal.ScreenUpdate.FullReplace,
				ScreenScroll:   terminal.ScreenUpdate.ScreenScroll,
				ChangedRows:    slices.Clone(terminal.ScreenUpdate.ChangedRows),
			},
		})
	}
	paneIDs := make([]string, 0, len(r.bindings))
	for paneID := range r.bindings {
		paneIDs = append(paneIDs, paneID)
	}
	sort.Slice(paneIDs, func(i, j int) bool {
		return shared.LessNumericStrings(paneIDs[i], paneIDs[j])
	})
	for _, paneID := range paneIDs {
		binding := r.bindings[paneID]
		if binding == nil {
			continue
		}
		visible.Bindings = append(visible.Bindings, VisiblePaneBinding{
			PaneID:         binding.PaneID,
			Role:           string(binding.Role),
			Connected:      binding.Connected,
			ViewportOffset: binding.Viewport.Offset,
		})
	}
	r.visibleCache = visible
	r.visibleVersion = r.version
	return visible
}
