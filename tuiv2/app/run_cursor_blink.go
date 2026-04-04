package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/render"
)

func startCursorBlinkForwarder(program *tea.Program, coordinator *render.Coordinator) func() {
	if program == nil || coordinator == nil {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(render.CursorBlinkInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if coordinator.NeedsCursorTicks() {
					program.Send(RenderTickMsg{})
				}
			}
		}
	}()

	return func() {
		close(done)
	}
}
