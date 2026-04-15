package app

import (
	"image/color"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/shared"
)

var hostThemeFlushDelay = 12 * time.Millisecond
var hostThemeBootstrapFlushDelay = 90 * time.Millisecond

func (m *Model) handleUIStateMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case pickerItemsLoadedMsg:
		if m.modalHost != nil {
			if m.modalHost.Picker == nil {
				m.modalHost.Picker = &modal.PickerState{}
			}
			m.modalHost.Picker.Items = typed.Items
			m.modalHost.Picker.ApplyFilter()
			if m.modalHost.Picker.Selected >= len(m.modalHost.Picker.VisibleItems()) {
				m.modalHost.Picker.Selected = 0
			}
			if m.modalHost.Session != nil {
				m.markModalReady(m.modalHost.Session.Kind, m.modalHost.Session.RequestID)
			}
		}
		m.render.Invalidate()
		return nil, true
	case terminalManagerItemsLoadedMsg:
		if m.terminalPage == nil {
			m.openTerminalPool()
		}
		m.terminalPage.Items = typed.Items
		m.terminalPage.ApplyFilter()
		m.render.Invalidate()
		return nil, true
	case terminalSizeLockToggledMsg:
		return m.showNotice(typed.Notice), true
	case orchestrator.KillTerminalEffect:
		return m.effectCmd(typed), true
	case EffectAppliedMsg:
		m.applyEffectSideState(typed.Effect)
		return nil, true
	case orchestrator.TerminalAttachedMsg:
		if service := m.terminalAttachService(); service != nil {
			return service.handleAttachedMsg(typed), true
		}
		return nil, true
	case paneAttachFailedMsg:
		m.clearPendingPaneAttach(typed.PaneID, typed.TerminalID)
		m.render.Invalidate()
		return m.showError(typed.Err), true
	case terminalAttachReadyMsg:
		return m.dequeueTerminalInputCmd(), true
	case orchestrator.SnapshotLoadedMsg:
		m.adjustCopyModeAfterSnapshotLoaded(typed.TerminalID)
		m.render.Invalidate()
		return m.maybeAutoFitFloatingPanesCmd(), true
	case hostDefaultColorsMsg:
		return m.enqueueHostThemeUpdate(typed.FG, typed.BG, nil), true
	case hostPaletteColorMsg:
		return m.enqueueHostThemeUpdate(nil, nil, map[int]color.Color{typed.Index: typed.Color}), true
	case hostThemeFlushMsg:
		m.flushPendingHostTheme()
		return nil, true
	case hostEmojiProbeMsg:
		if m.runtime == nil || !m.hostEmojiProbePending || m.cursorOut == nil || typed.Attempt <= 0 {
			return nil, true
		}
		m.debugLog("host_emoji_probe_send", "attempt", typed.Attempt)
		if err := m.cursorOut.WriteControlSequence(hostEmojiVariationProbeSequence); err != nil {
			m.debugLog("host_emoji_probe_write_failed", "attempt", typed.Attempt, "err", err)
			m.hostEmojiProbePending = false
			return nil, true
		}
		if typed.Attempt >= hostEmojiProbeMaxAttempts {
			return hostEmojiProbeGiveUpCmd(hostEmojiProbeRetryDelay), true
		}
		return m.hostEmojiProbeCmd(typed.Attempt+1, hostEmojiProbeRetryDelay), true
	case hostEmojiProbeGiveUpMsg:
		if !m.hostEmojiProbePending {
			return nil, true
		}
		m.debugLog("host_emoji_probe_give_up")
		m.hostEmojiProbePending = false
		if m.runtime != nil {
			m.runtime.SetHostAmbiguousEmojiVariationSelectorMode(shared.AmbiguousEmojiVariationSelectorStrip)
		}
		return nil, true
	case hostCursorPositionMsg:
		if m.runtime == nil || !m.hostEmojiProbePending {
			return nil, true
		}
		mode, ok := hostEmojiProbeModeFromReportedColumn(typed.X)
		if !ok {
			m.debugLog("host_emoji_probe_response_ignored", "x", typed.X, "y", typed.Y)
			return nil, true
		}
		m.hostEmojiProbePending = false
		m.debugLog("host_emoji_probe_response", "x", typed.X, "y", typed.Y, "mode", mode)
		m.runtime.SetHostAmbiguousEmojiVariationSelectorMode(mode)
		return nil, true
	default:
		return nil, false
	}
}

func (m *Model) enqueueHostThemeUpdate(fg, bg color.Color, palette map[int]color.Color) tea.Cmd {
	if m == nil || m.runtime == nil {
		return nil
	}
	if fg != nil {
		m.pendingHostDefaultFG = fg
		m.hostThemeBootstrapSeenFG = true
	}
	if bg != nil {
		m.pendingHostDefaultBG = bg
		m.hostThemeBootstrapSeenBG = true
	}
	if len(palette) > 0 {
		if m.pendingHostPalette == nil {
			m.pendingHostPalette = make(map[int]color.Color)
		}
		if m.hostThemeBootstrapPalette == nil {
			m.hostThemeBootstrapPalette = make(map[int]struct{})
		}
		for index, c := range palette {
			if c == nil {
				continue
			}
			m.pendingHostPalette[index] = c
			m.hostThemeBootstrapPalette[index] = struct{}{}
		}
	}
	if m.hostThemeBootstrapPending &&
		m.hostThemeBootstrapSeenFG &&
		m.hostThemeBootstrapSeenBG &&
		len(m.hostThemeBootstrapPalette) >= m.hostThemeBootstrapPaletteN {
		m.flushPendingHostTheme()
		return nil
	}
	if m.hostThemeFlushPending {
		return nil
	}
	m.hostThemeFlushPending = true
	delay := hostThemeFlushDelay
	if m.hostThemeBootstrapPending {
		delay = hostThemeBootstrapFlushDelay
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return hostThemeFlushMsg{}
	})
}

func (m *Model) flushPendingHostTheme() {
	if m == nil || m.runtime == nil {
		return
	}
	bootstrap := m.hostThemeBootstrapPending
	m.hostThemeFlushPending = false
	if bootstrap {
		m.runtime.ApplyHostThemeSilently(m.pendingHostDefaultFG, m.pendingHostDefaultBG, m.pendingHostPalette)
	} else {
		m.runtime.ApplyHostTheme(m.pendingHostDefaultFG, m.pendingHostDefaultBG, m.pendingHostPalette)
	}
	m.pendingHostDefaultFG = nil
	m.pendingHostDefaultBG = nil
	if m.pendingHostPalette != nil {
		clear(m.pendingHostPalette)
	}
	m.hostThemeBootstrapPending = false
	m.hostThemeBootstrapPaletteN = 0
	m.hostThemeBootstrapSeenFG = false
	m.hostThemeBootstrapSeenBG = false
	if m.hostThemeBootstrapPalette != nil {
		clear(m.hostThemeBootstrapPalette)
	}
}
