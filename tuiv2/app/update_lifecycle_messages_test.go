package app

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestHandleLifecycleMessageInvalidateClearsPendingAndReschedulesDeferred(t *testing.T) {
	model := setupModel(t, modelOpts{})
	model.invalidatePending.Store(true)
	model.invalidateDeferred.Store(true)

	cmd, handled := model.handleLifecycleMessage(InvalidateMsg{})
	if !handled {
		t.Fatal("expected InvalidateMsg handled")
	}
	if model.invalidatePending.Load() {
		t.Fatal("expected invalidate pending cleared")
	}
	if model.invalidateDeferred.Load() {
		t.Fatal("expected deferred invalidate flag cleared after handling")
	}
	_ = cmd
}

func TestHandleLifecycleMessageWindowSizeUpdatesDimensions(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleLifecycleMessage(tea.WindowSizeMsg{Width: 140, Height: 42})
	if !handled || cmd == nil {
		t.Fatalf("expected WindowSizeMsg handled with follow-up cmd, got handled=%v cmd=%#v", handled, cmd)
	}
	if model.width != 140 || model.height != 42 {
		t.Fatalf("expected model dimensions updated, got width=%d height=%d", model.width, model.height)
	}
}

func TestHandleLifecycleMessageErrorUsesShowError(t *testing.T) {
	model := setupModel(t, modelOpts{})

	cmd, handled := model.handleLifecycleMessage(errors.New("boom"))
	if !handled || cmd == nil {
		t.Fatalf("expected error handled via showError, got handled=%v cmd=%#v", handled, cmd)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected clearError follow-up from showError")
	}
}

func TestHandleLifecycleMessageRenderTickHandledWithAndWithoutRenderer(t *testing.T) {
	model := setupModel(t, modelOpts{})
	cmd, handled := model.handleLifecycleMessage(RenderTickMsg{})
	if !handled || cmd != nil {
		t.Fatalf("expected RenderTickMsg handled without follow-up cmd, got handled=%v cmd=%#v", handled, cmd)
	}

	model.render = nil
	cmd, handled = model.handleLifecycleMessage(RenderTickMsg{})
	if !handled || cmd != nil {
		t.Fatalf("expected RenderTickMsg handled safely with nil renderer, got handled=%v cmd=%#v", handled, cmd)
	}
}

func TestHandleLifecycleMessageFallsThroughForUnknownMsg(t *testing.T) {
	model := setupModel(t, modelOpts{})
	cmd, handled := model.handleLifecycleMessage(workbench.Rect{W: 1, H: 1})
	if handled || cmd != nil {
		t.Fatalf("expected unknown lifecycle msg to fall through, got handled=%v cmd=%#v", handled, cmd)
	}
}
