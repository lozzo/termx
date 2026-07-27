package shortcut

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	actiondomain "github.com/anytty/anytty/tui/action"
)

func TestBindingPoliciesOwnSceneAndVisibility(t *testing.T) {
	cases := []struct {
		source string
		scene  string
	}{
		{source: "system.open_terminal_picker", scene: "floating"},
		{source: "system.open_workbench_tree", scene: "workspace"},
		{source: "menu.pane", scene: "global"},
	}
	for _, tc := range cases {
		policy, invocation, _, ok := PolicyForSource(tc.source)
		if !ok || !AllowsScene(invocation.ID, tc.scene) || policy.Footer == "" || policy.Help == "" {
			t.Fatalf("source=%q scene=%q invocation=%#v policy=%#v", tc.source, tc.scene, invocation, policy)
		}
	}
	if AllowsScene(actiondomain.ID("panel.close"), "workspace") {
		t.Fatal("panel.close must not bind outside the panel scene")
	}
}

func TestSceneRegistryOwnsMenuActionReverseLookup(t *testing.T) {
	seen := map[actiondomain.ID]SceneID{}
	for _, scene := range Scenes() {
		if scene.MenuAction == "" {
			continue
		}
		resolved, ok := SceneByMenuAction(scene.MenuAction)
		if !ok || resolved.ID != scene.ID {
			t.Fatalf("menu action %q must resolve to scene %q, got %#v ok=%v", scene.MenuAction, scene.ID, resolved, ok)
		}
		if previous, exists := seen[scene.MenuAction]; exists {
			t.Fatalf("menu action %q is shared by scenes %q and %q", scene.MenuAction, previous, scene.ID)
		}
		seen[scene.MenuAction] = scene.ID
	}
	if _, ok := SceneByMenuAction("panel.close"); ok {
		t.Fatal("non-menu action must not resolve to a scene")
	}
}

func TestShortcutPackageDoesNotRedeclareActionDomainOrClickPolicy(t *testing.T) {
	forbidden := map[string]bool{"Spec": true, "Invocation": true, "ParamSpec": true, "ClickPolicy": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, declaration := range parsed.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, item := range generic.Specs {
				typeSpec := item.(*ast.TypeSpec)
				if forbidden[typeSpec.Name.Name] {
					t.Fatalf("shortcut must not redeclare action/render owner type %s in %s", typeSpec.Name.Name, file)
				}
			}
		}
	}
}
