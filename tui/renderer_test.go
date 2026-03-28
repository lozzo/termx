package tui

import (
	"strings"
	"testing"
	"time"
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

func TestModelViewUsesRendererBackedPath(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 40
	model.renderDirty = true

	out := model.View()
	if out == "" {
		t.Fatal("expected rendered output")
	}
	if model.renderCache == "" {
		t.Fatal("expected renderer-backed view to finish frame into cache")
	}
}

func TestRendererFinishFrameCachesAndClearsDirty(t *testing.T) {
	renderer := NewRenderer(nil, nil)
	model := &Model{renderDirty: true, timeNow: func() time.Time { return time.Unix(100, 0) }}

	out := renderer.FinishFrame(model, "frame")

	if out != "frame" {
		t.Fatalf("expected frame output, got %q", out)
	}
	if model.renderCache != "frame" {
		t.Fatalf("expected render cache to be updated, got %q", model.renderCache)
	}
	if model.renderDirty {
		t.Fatal("expected render dirty flag cleared")
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
