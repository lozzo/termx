package runtime

import (
	"context"
	"fmt"
	"hash/fnv"
	"image/color"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/vterm"
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

	version          uint64
	visibleVersion   uint64
	visibleCache     *VisibleRuntime
	recentInputAt    atomic.Int64
	inputBypassArmed atomic.Bool
}

type debugTraceLabelSetter interface {
	SetDebugTraceLabel(label string)
}

const interactiveLatencyWindow = 24 * time.Millisecond

var runtimeDebugTracePath = strings.TrimSpace(os.Getenv("TERMX_DEBUG_TRACE"))

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
	appendSharedTerminalTrace("runtime.unbind.begin", terminal, "pane=%s", paneID)
	terminal.BoundPaneIDs = removeBoundPaneID(terminal.BoundPaneIDs, paneID)
	if terminal.OwnerPaneID == paneID {
		terminal.OwnerPaneID = ""
		terminal.RequiresExplicitOwner = true
	}
	if terminal.ControlPaneID == paneID {
		terminal.ControlPaneID = ""
	}
	r.syncTerminalOwnership(terminal)
	appendSharedTerminalTrace("runtime.unbind.end", terminal, "pane=%s", paneID)
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
			terminal.Surface = materializeSurfaceFromVTerm(terminal.VTerm)
			if terminal.Surface != nil && terminal.SurfaceVersion == 0 {
				terminal.SurfaceVersion = 1
				terminal.PublishedGeneration = 1
				terminal.PublishGeneration = 1
			}
		}
	}
	if setter, ok := terminal.VTerm.(debugTraceLabelSetter); ok {
		setter.SetDebugTraceLabel("client:" + terminal.TerminalID)
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
	if r == nil {
		return
	}
	nextFG := r.hostDefaultFG
	nextBG := r.hostDefaultBG
	if fg != nil {
		nextFG = colorToHex(fg)
	}
	if bg != nil {
		nextBG = colorToHex(bg)
	}
	if nextFG == r.hostDefaultFG && nextBG == r.hostDefaultBG {
		return
	}
	r.hostDefaultFG = nextFG
	r.hostDefaultBG = nextBG
	for _, terminalID := range r.registry.IDs() {
		terminal := r.registry.Get(terminalID)
		if terminal == nil || terminal.VTerm == nil {
			continue
		}
		terminal.VTerm.SetDefaultColors(nextFG, nextBG)
	}
	r.invalidate()
}

func (r *Runtime) SetHostPaletteColor(index int, c color.Color) {
	if r == nil || c == nil || index < 0 || index > 255 {
		return
	}
	if r.hostPalette == nil {
		r.hostPalette = make(map[int]string)
	}
	value := colorToHex(c)
	if r.hostPalette[index] == value {
		return
	}
	r.hostPalette[index] = value
	for _, terminalID := range r.registry.IDs() {
		terminal := r.registry.Get(terminalID)
		if terminal == nil || terminal.VTerm == nil {
			continue
		}
		terminal.VTerm.SetIndexedColor(index, value)
	}
	r.invalidate()
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
			PaneID:    binding.PaneID,
			Role:      string(binding.Role),
			Connected: binding.Connected,
		})
	}
	r.visibleCache = visible
	r.visibleVersion = r.version
	r.appendDebugTrace(visible)
	return visible
}

func (r *Runtime) appendDebugTrace(visible *VisibleRuntime) {
	if r == nil || runtimeDebugTracePath == "" || visible == nil {
		return
	}
	var b strings.Builder
	b.WriteString(time.Now().Format(time.RFC3339Nano))
	b.WriteString(" runtime.visible")
	for _, term := range visible.Terminals {
		b.WriteString(" term=")
		b.WriteString(term.TerminalID)
		b.WriteString(" surfVer=")
		b.WriteString(fmt.Sprintf("%d", term.SurfaceVersion))
		b.WriteString(" snapVer=")
		b.WriteString(fmt.Sprintf("%d", term.SnapshotVersion))
		if term.Surface != nil {
			size := term.Surface.Size()
			cursor := term.Surface.Cursor()
			b.WriteString(" size=")
			b.WriteString(fmt.Sprintf("%dx%d", size.Cols, size.Rows))
			b.WriteString(" cursor=")
			b.WriteString(fmt.Sprintf("%d,%d", cursor.Row, cursor.Col))
			b.WriteString(" hash=")
			b.WriteString(surfaceDigest(term.Surface))
		}
	}
	b.WriteByte('\n')
	appendRuntimeTraceLine(b.String())
}

func appendRuntimeTraceLine(line string) {
	if runtimeDebugTracePath == "" || line == "" {
		return
	}
	f, err := os.OpenFile(runtimeDebugTracePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func surfaceDigest(surface TerminalSurface) string {
	if surface == nil {
		return "nil"
	}
	h := fnv.New64a()
	size := surface.Size()
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%d|", size.Cols, size.Rows)))
	for i := 0; i < surface.TotalRows(); i++ {
		row := surface.Row(i)
		for _, cell := range row {
			_, _ = h.Write([]byte(cell.Content))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write([]byte(fmt.Sprintf("%d|%s|%s|%t|%t|%t|%t|", cell.Width, cell.Style.FG, cell.Style.BG, cell.Style.Bold, cell.Style.Italic, cell.Style.Underline, cell.Style.Reverse)))
		}
		_, _ = h.Write([]byte{'\n'})
	}
	return fmt.Sprintf("%016x", h.Sum64())
}
