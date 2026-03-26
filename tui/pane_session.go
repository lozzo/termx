package tui

import localvterm "github.com/lozzow/termx/vterm"

type PaneSession struct {
	TerminalID     string
	Channel        uint16
	AttachMode     string
	VTerm          *localvterm.VTerm
	DefaultFG      string
	DefaultBG      string
	ResizeAcquired bool
	stopStream     func()

	cellCache       [][]drawCell
	cellVersion     uint64
	viewportCache   [][]drawCell
	viewportOffset  Point
	viewportWidth   int
	viewportHeight  int
	viewportVersion uint64
	renderDirty     bool
	live            bool
	syncLost        bool
	droppedBytes    uint64
	recovering      bool
	catchingUp      bool
	dirtyTicks      int
	cleanTicks      int
	skipTick        bool
	dirtyRowsKnown  bool
	dirtyRowStart   int
	dirtyRowEnd     int
	dirtyColsKnown  bool
	dirtyColStart   int
	dirtyColEnd     int
}

func (s *PaneSession) IsRenderDirty() bool {
	return s != nil && s.renderDirty
}

func (s *PaneSession) MarkRenderDirty() {
	if s == nil {
		return
	}
	s.renderDirty = true
}

func (s *PaneSession) ClearRenderDirty() {
	if s == nil {
		return
	}
	s.renderDirty = false
}

func (s *PaneSession) IsSyncLost() bool {
	return s != nil && s.syncLost
}

func (s *PaneSession) SetSyncLost(value bool) {
	if s == nil {
		return
	}
	s.syncLost = value
}

func (s *PaneSession) DroppedBytes() uint64 {
	if s == nil {
		return 0
	}
	return s.droppedBytes
}

func (s *PaneSession) AddDroppedBytes(value uint64) {
	if s == nil {
		return
	}
	s.droppedBytes += value
}

func (s *PaneSession) SetDroppedBytes(value uint64) {
	if s == nil {
		return
	}
	s.droppedBytes = value
}

func (s *PaneSession) IsRecovering() bool {
	return s != nil && s.recovering
}

func (s *PaneSession) SetRecovering(value bool) {
	if s == nil {
		return
	}
	s.recovering = value
}
