package input

import (
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/shortcut"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestDefaultShortcutCatalogIsAllowedByDomainRegistry(t *testing.T) {
	for _, entry := range ShortcutEntriesForConfig(state.TUIShortcutConfig{}) {
		invocation, spec, err := shortcut.ParseInvocation(entry.ActionID)
		if err != nil {
			t.Fatalf("default shortcut %s.%s action=%q: %v", entry.Scene, entry.Key, entry.ActionID, err)
		}
		if !spec.AllowsScene(entry.Scene) {
			t.Fatalf("default shortcut %s.%s invocation=%#v is not allowed by spec %#v", entry.Scene, entry.Key, invocation, spec)
		}
	}
}
