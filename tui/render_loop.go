package tui

import "time"

type RenderLoop struct {
	renderer *Renderer
	model    *Model
}

func NewRenderLoop(renderer *Renderer) *RenderLoop {
	return &RenderLoop{renderer: renderer}
}

func (l *RenderLoop) bindModel(model *Model) {
	if l == nil {
		return
	}
	l.model = model
}

func (l *RenderLoop) Renderer() *Renderer {
	if l == nil {
		return nil
	}
	return l.renderer
}

func (l *RenderLoop) invalidateRender() {
	m := l.model
	if m == nil {
		return
	}
	m.renderDirty = true
}

func (l *RenderLoop) startTicker() {
	m := l.model
	if m == nil || m.program == nil || m.renderTickerRunning || m.renderInterval <= 0 {
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

func (l *RenderLoop) scheduleRender() {
	m := l.model
	if m == nil {
		return
	}
	if !m.renderBatching {
		l.invalidateRender()
		return
	}
	m.renderPending.Store(true)
}

func (l *RenderLoop) flushPendingRender() {
	m := l.model
	if m == nil {
		return
	}
	if !m.renderBatching {
		l.invalidateRender()
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
	l.invalidateRender()
}

func (l *RenderLoop) requestInteractiveRender() {
	m := l.model
	if m == nil {
		return
	}
	if !m.renderBatching || m.program == nil {
		l.invalidateRender()
		return
	}
	now := m.now()
	interval := m.currentRenderInterval(now)
	if interval <= 0 || m.renderLastFlush.IsZero() || now.Sub(m.renderLastFlush) >= interval {
		l.invalidateRender()
		m.renderLastFlush = now
		return
	}
	m.renderPending.Store(true)
}

func (l *RenderLoop) stopTicker() {
	m := l.model
	if m == nil {
		return
	}
	if m.renderTickerStop != nil {
		close(m.renderTickerStop)
		m.renderTickerStop = nil
	}
	m.renderTickerRunning = false
}
