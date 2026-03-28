package tui

import (
	"strings"
	"testing"
)

func TestNewRendererHoldsWorkbenchAndStore(t *testing.T) {
	workbench := NewWorkbench(Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0})
	store := NewTerminalStore()
	renderer := NewRenderer(workbench, store)

	if renderer == nil {
		t.Fatal("expected renderer")
	}
	if renderer.Workbench() != workbench {
		t.Fatal("expected renderer to hold workbench reference")
	}
	if renderer.TerminalStore() != store {
		t.Fatal("expected renderer to hold terminal store reference")
	}
}

func TestRendererViewAssemblesTabBodyAndStatus(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	renderer := model.app.Renderer()
	if renderer == nil {
		t.Fatal("expected model app renderer")
	}

	got := renderer.Render(model)
	want := strings.Join([]string{model.renderTabBar(), model.renderContentBody(), model.renderStatus()}, "\n")
	if got != want {
		t.Fatalf("expected renderer to assemble top-level frame\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestRendererCanServeCachedFrame(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	model.showHelp = true
	model.renderCache = "cached frame"
	model.renderDirty = false
	model.renderBatching = true

	renderer := model.app.Renderer()
	if renderer == nil {
		t.Fatal("expected model app renderer")
	}

	if got := renderer.CachedFrame(model); got != "cached frame" {
		t.Fatalf("expected cached frame, got %q", got)
	}

	model.renderDirty = true
	if got := renderer.CachedFrame(model); got != "" {
		t.Fatalf("expected dirty model to bypass cache, got %q", got)
	}
}
