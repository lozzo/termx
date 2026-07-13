package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actiondomain "github.com/lozzow/termx/tui/action"
	"github.com/lozzow/termx/tui/input"
	"github.com/lozzow/termx/tui/render"
	"github.com/lozzow/termx/tui/shortcut"
)

func TestShortcutDocumentationSummaryMatchesRuntimeCatalog(t *testing.T) {
	inventory, err := os.ReadFile(filepath.Join("..", "docs", "shortcut-inventory.md"))
	if err != nil {
		t.Fatalf("read shortcut inventory: %v", err)
	}
	want := fmt.Sprintf(
		"当前运行基线：`default_entries=%d; routed_bindings=%d; action_specs=%d; render_projections=%d; scenes=%d`。",
		len(shortcut.DefaultBindings()),
		len(input.BindingCatalog()),
		len(actiondomain.Specs()),
		len(render.ProjectionCatalog()),
		len(shortcut.Scenes()),
	)
	if !strings.Contains(string(inventory), want) {
		t.Fatalf("shortcut inventory summary drifted from runtime catalog; want line:\n%s", want)
	}
}

func TestREADMEReferencesBothLoadableShortcutExamples(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	for _, path := range []string{"tui/docs/tui-v3.example.yaml", "tui/docs/config.example.yaml"} {
		if !strings.Contains(string(readme), path) {
			t.Fatalf("README must reference loadable shortcut example %s", path)
		}
	}
	for _, contract := range []string{
		"ctrl-`、`alt-`、`shift-",
		"page-up`（别名 `pgup`）",
		"page-down`（别名 `pgdn`）",
		"`f1` 至 `f12`",
		"`enter` 与 `return`",
		"`R` 与 `r` 是不同 binding",
		"显式 Shift 时写 `ctrl-shift-a`",
		"完整替换默认 bindings",
	} {
		if !strings.Contains(string(readme), contract) {
			t.Errorf("README must describe shortcut runtime contract %q", contract)
		}
	}
}

func TestShortcutExamplesDescribeRuntimeConfigurationContract(t *testing.T) {
	paths := []string{
		filepath.Join("..", "docs", "tui-v3.example.yaml"),
		filepath.Join("..", "docs", "config.example.yaml"),
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read shortcut example %s: %v", path, err)
		}
		text := string(content)
		for _, contract := range []string{
			"shortcuts: {}",
			"继承默认 bindings",
			"scene catalog",
			"完整替换默认 bindings",
		} {
			if !strings.Contains(text, contract) {
				t.Errorf("shortcut example %s must describe %q runtime contract", path, contract)
			}
		}
	}

	configExample, err := os.ReadFile(filepath.Join("..", "docs", "config.example.yaml"))
	if err != nil {
		t.Fatalf("read config example: %v", err)
	}
	for _, contract := range []string{"canonical action identity 由 tui/action 持有", "render 只持有 ProjectionSpec"} {
		if !strings.Contains(string(configExample), contract) {
			t.Errorf("config example must describe canonical ownership contract %q", contract)
		}
	}
}
