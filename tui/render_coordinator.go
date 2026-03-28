package tui

import (
	"fmt"
	"strings"
	"time"
)

func (m *Model) startRenderTicker() {
	if loop := m.renderLoop(); loop != nil {
		loop.startTicker()
		return
	}
	if m.program == nil || m.renderTickerRunning || m.renderInterval <= 0 {
		return
	}
	stop := make(chan struct{})
	m.renderTickerStop = stop
	m.renderTickerRunning = true
	interval := minPositiveDuration(m.renderFastInterval, m.renderInterval)
	if interval <= 0 {
		interval = m.renderInterval
	}
	statsInterval := m.renderStatsInterval
	program := m.program
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var statsTicker *time.Ticker
		if statsInterval > 0 {
			statsTicker = time.NewTicker(statsInterval)
			defer statsTicker.Stop()
		}
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !m.renderPending.Load() {
					continue
				}
				program.Send(renderTickMsg{})
			case <-tickerChan(statsTicker):
				m.logRenderStats()
			}
		}
	}()
}

func (m *Model) invalidateRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.invalidateRender()
		return
	}
	m.renderDirty = true
}

func (m *Model) scheduleRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.scheduleRender()
		return
	}
	if !m.renderBatching {
		m.invalidateRender()
		return
	}
	m.renderPending.Store(true)
}

func (m *Model) flushPendingRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.flushPendingRender()
		return
	}
	if !m.renderBatching {
		m.invalidateRender()
		return
	}
	now := m.now()
	interval := m.currentRenderInterval(now)
	if interval > 0 && !m.renderLastFlush.IsZero() && now.Sub(m.renderLastFlush) < interval {
		return
	}
	if !m.updateBackpressureState() {
		return
	}
	if !m.renderPending.Load() && !m.anyPaneDirty() {
		return
	}
	m.renderPending.Store(false)
	m.renderLastFlush = now
	m.invalidateRender()
}

func (m *Model) now() time.Time {
	if m != nil && m.timeNow != nil {
		return m.timeNow()
	}
	return time.Now()
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

func (m *Model) noteInteraction() {
	if m == nil || m.renderInteractiveWindow <= 0 {
		return
	}
	until := m.now().Add(m.renderInteractiveWindow)
	if until.After(m.renderInteractiveUntil) {
		m.renderInteractiveUntil = until
	}
}

func (m *Model) requestInteractiveRender() {
	if loop := m.renderLoop(); loop != nil {
		loop.requestInteractiveRender()
		return
	}
	if !m.renderBatching || m.program == nil {
		m.invalidateRender()
		return
	}
	now := m.now()
	interval := m.currentRenderInterval(now)
	if interval <= 0 || m.renderLastFlush.IsZero() || now.Sub(m.renderLastFlush) >= interval {
		m.invalidateRender()
		m.renderLastFlush = now
		return
	}
	m.renderPending.Store(true)
}

func (m *Model) logRenderStats() {
	if m == nil || m.logger == nil || m.renderStatsInterval <= 0 {
		return
	}
	viewCalls := m.renderViewCalls.Swap(0)
	frames := m.renderFrames.Swap(0)
	cacheHits := m.renderCacheHits.Swap(0)
	fps := 0.0
	if seconds := m.renderStatsInterval.Seconds(); seconds > 0 {
		fps = float64(frames) / seconds
	}
	m.logger.Info("tui render stats",
		"window", m.renderStatsInterval.String(),
		"view_calls", viewCalls,
		"frames", frames,
		"cache_hits", cacheHits,
		"fps", fmt.Sprintf("%.2f", fps),
	)
}

func (m *Model) currentRenderInterval(now time.Time) time.Duration {
	interval := m.renderInterval
	if interval <= 0 {
		interval = m.renderFastInterval
	}
	if !m.renderInteractiveUntil.IsZero() && now.Before(m.renderInteractiveUntil) && m.renderFastInterval > 0 && (interval <= 0 || m.renderFastInterval < interval) {
		return m.renderFastInterval
	}
	return interval
}

func minPositiveDuration(a, b time.Duration) time.Duration {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func (m *Model) anyPaneDirty() bool {
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane != nil && pane.renderDirty {
				return true
			}
		}
	}
	return false
}

func (m *Model) updateBackpressureState() bool {
	shouldRender := false
	for _, tab := range m.workspace.Tabs {
		if tab == nil {
			continue
		}
		for _, pane := range tab.Panes {
			if pane == nil {
				continue
			}
			if pane.renderDirty {
				pane.dirtyTicks++
				pane.cleanTicks = 0
				if pane.dirtyTicks >= 30 {
					pane.catchingUp = true
				}
				if pane.catchingUp {
					pane.skipTick = !pane.skipTick
					if pane.skipTick {
						continue
					}
				}
				shouldRender = true
				continue
			}

			pane.dirtyTicks = 0
			if pane.catchingUp {
				pane.cleanTicks++
				if pane.cleanTicks >= 5 {
					pane.catchingUp = false
					pane.cleanTicks = 0
					pane.skipTick = false
				}
			}
		}
	}
	return shouldRender || !m.anyPaneDirty()
}

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		m.renderViewCalls.Add(1)
		m.renderFrames.Add(1)
		return "loading..."
	}
	m.renderViewCalls.Add(1)

	renderer := (*Renderer)(nil)
	if m.app != nil {
		renderer = m.app.Renderer()
	}
	if renderer != nil {
		if cached := renderer.CachedFrame(m); cached != "" {
			return cached
		}
	} else {
		if !m.renderDirty && m.renderCache != "" && (m.workspacePicker != nil || m.terminalManager != nil || m.terminalPicker != nil || m.showHelp || m.prompt != nil) {
			m.renderCacheHits.Add(1)
			return m.renderCache
		}

		if m.renderBatching && !m.renderDirty && m.renderCache != "" {
			if m.program != nil || !m.anyPaneDirty() {
				m.renderCacheHits.Add(1)
				return m.renderCache
			}
		}
	}

	var out string
	if m.workspacePicker != nil {
		out = m.renderWorkspacePicker()
		return m.finishRenderedFrame(out)
	}

	if m.terminalManager != nil {
		out = m.renderTerminalManager()
		return m.finishRenderedFrame(out)
	}

	if m.terminalPicker != nil {
		out = m.renderTerminalPicker()
		return m.finishRenderedFrame(out)
	}

	if m.showHelp {
		out = m.renderHelpScreen()
		return m.finishRenderedFrame(out)
	}

	if m.prompt != nil && m.prompt.Kind != "command" {
		out = m.renderPromptScreen()
		return m.finishRenderedFrame(out)
	}

	if renderer != nil {
		out = renderer.Render(m)
	} else {
		out = strings.Join([]string{m.renderTabBar(), m.renderContentBody(), m.renderStatus()}, "\n")
	}
	return m.finishRenderedFrame(out)
}

func (m *Model) finishRenderedFrame(out string) string {
	renderer := (*Renderer)(nil)
	if m.app != nil {
		renderer = m.app.Renderer()
	}
	if renderer != nil {
		return renderer.FinishFrame(m, out)
	}
	m.renderCache = out
	m.renderDirty = false
	m.renderLastFlush = m.now()
	m.renderFrames.Add(1)
	return out
}
