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
