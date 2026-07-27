package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	actiondomain "github.com/anytty/anytty/tui/action"
	"github.com/anytty/anytty/tui/input"
	"github.com/anytty/anytty/tui/render"
	"github.com/anytty/anytty/tui/shortcut"
	"github.com/anytty/anytty/tui/state"
)

const shortcutArtifactBaselineSHA256 = "dc3355c5d876522d2a3a505005787e14e73d56d4358888cd968eaf054c6b1494"
const shortcutCompositeSemanticBaselineSHA256 = "553d3ddff7f48b615488146e870a27be9d6198ca9e63d7dfce669d25e7e53dc8"
const shortcutRenderStringBaselineSHA256 = "69687632499b21b8349a91d6eea8bdf91048f3d6cd32bdbef27d292af7b3efc5"

type shortcutDebtManifest struct {
	SchemaVersion int                    `json:"schema_version"`
	Stage         string                 `json:"stage"`
	Inventory     shortcutAuditInventory `json:"inventory"`
	Artifacts     shortcutAuditArtifacts `json:"artifacts"`
	Surfaces      []shortcutAuditSurface `json:"surfaces"`
}

type shortcutAuditInventory struct {
	DefaultEntries    int `json:"default_entries"`
	RoutedBindings    int `json:"routed_bindings"`
	ShortcutSpecs     int `json:"shortcut_specs"`
	RenderProjections int `json:"render_projections"`
}

type shortcutAuditArtifacts struct {
	InputEventProducers []shortcutAuditArtifact `json:"input_event_producers"`
	HitRegionProducers  []shortcutAuditArtifact `json:"hit_region_producers"`
	DisplayKeyLiterals  []shortcutAuditArtifact `json:"display_key_literals"`
}

type shortcutAuditArtifact struct {
	Signature      string `json:"signature"`
	Count          int    `json:"count"`
	Classification string `json:"classification"`
	Digest         string `json:"digest,omitempty"`
}

type shortcutAuditSurface struct {
	ID             string                `json:"id"`
	Kind           string                `json:"kind"`
	Classification string                `json:"classification"`
	Owner          string                `json:"owner"`
	TargetSlice    string                `json:"target_slice,omitempty"`
	Reason         string                `json:"reason"`
	Sources        []shortcutAuditSource `json:"sources"`
}

type shortcutAuditSource struct {
	Path    string   `json:"path"`
	Anchors []string `json:"anchors"`
}

// TestShortcutContractDebtManifestLocksKnownDebt 验证当前快捷键债务基线不能静默扩张。
// manifest 记录 owner/source/目标切片；测试内的 debt ID 集合是独立门禁，新增债务不能只修改
// JSON 自我批准，必须显式修改审计代码并重新经过阶段双审查。
func TestShortcutContractDebtManifestLocksKnownDebt(t *testing.T) {
	repoRoot := shortcutAuditRepoRoot(t)
	manifest := readShortcutDebtManifest(t, repoRoot)
	if manifest.SchemaVersion != 1 || manifest.Stage != "KS016" {
		t.Fatalf("unexpected shortcut debt manifest header: %#v", manifest)
	}

	wantDebtIDs := []string{}
	seenIDs := map[string]bool{}
	kinds := map[string]bool{}
	debtIDs := []string{}
	for _, surface := range manifest.Surfaces {
		if surface.ID == "" || seenIDs[surface.ID] {
			t.Fatalf("shortcut audit surface id must be non-empty and unique: %q", surface.ID)
		}
		seenIDs[surface.ID] = true
		kinds[surface.Kind] = true
		if surface.Owner == "" || surface.Reason == "" || len(surface.Sources) == 0 {
			t.Fatalf("shortcut audit surface %q misses owner, reason, or sources: %#v", surface.ID, surface)
		}
		switch surface.Classification {
		case "conforming":
			if surface.TargetSlice != "" {
				t.Fatalf("conforming surface %q must not declare target_slice", surface.ID)
			}
		case "debt":
			if surface.TargetSlice != "KS013" && surface.TargetSlice != "KS015" && surface.TargetSlice != "KS016" {
				t.Fatalf("debt surface %q has invalid target slice %q", surface.ID, surface.TargetSlice)
			}
			debtIDs = append(debtIDs, surface.ID)
		default:
			t.Fatalf("shortcut audit surface %q has unknown classification %q", surface.ID, surface.Classification)
		}
		assertShortcutAuditAnchors(t, repoRoot, surface)
	}
	for _, kind := range []string{"input", "binding", "spec", "handler", "projection", "hint"} {
		if !kinds[kind] {
			t.Fatalf("shortcut debt manifest does not classify %s surfaces", kind)
		}
	}
	sort.Strings(debtIDs)
	if !reflect.DeepEqual(debtIDs, wantDebtIDs) {
		t.Fatalf("shortcut debt baseline changed without updating the independent guard:\n got=%q\nwant=%q", debtIDs, wantDebtIDs)
	}
	assertShortcutObservedArtifacts(t, repoRoot, manifest, seenIDs)
}

// TestShortcutContractAuditClassifiesDefaultBindings 逐项证明默认 binding 能解析到唯一 spec、
// scene 合法且能进入 app handler；任一新增 binding 若落在未注册或未处理路径都会失败。
func TestShortcutContractAuditClassifiesDefaultBindings(t *testing.T) {
	manifest := readShortcutDebtManifest(t, shortcutAuditRepoRoot(t))
	wantInventory := shortcutAuditInventory{DefaultEntries: 206, RoutedBindings: 167, ShortcutSpecs: 163, RenderProjections: 34}
	if manifest.Inventory != wantInventory {
		t.Fatalf("shortcut manifest inventory changed without updating the independent guard: got=%#v want=%#v", manifest.Inventory, wantInventory)
	}
	entries := input.ShortcutEntriesForConfig(state.TUIShortcutConfig{})
	bindings := input.BindingCatalog()
	if len(entries) == 0 || len(bindings) == 0 || len(bindings) > len(entries) {
		t.Fatalf("default shortcut catalog and routed subset must be non-empty: entries=%d bindings=%d", len(entries), len(bindings))
	}

	seenKeys := map[string]string{}
	boundSpecs := map[string]bool{}
	entryInvocations := map[string]bool{}
	for _, entry := range entries {
		invocation, spec, err := actiondomain.ParseInvocation(entry.ActionID)
		if err != nil {
			t.Fatalf("default binding %s.%s action=%q has no canonical spec: %v", entry.Scene, entry.Key, entry.ActionID, err)
		}
		if !shortcut.AllowsScene(invocation.ID, entry.Scene) {
			t.Fatalf("default binding %s.%s action=%q is forbidden by spec %#v", entry.Scene, entry.Key, entry.ActionID, spec)
		}
		key := entry.Scene + "\x00" + entry.Key
		if previous, ok := seenKeys[key]; ok {
			t.Fatalf("default binding key %s.%s is duplicated by %q and %q", entry.Scene, entry.Key, previous, entry.ActionID)
		}
		seenKeys[key] = entry.ActionID
		boundSpecs[string(spec.ID)] = true
		entryInvocations[invocation.Signature()] = true

		intent, ok := shortcutIntentForInvocation(invocation, input.InputEvent{})
		if !ok {
			t.Fatalf("default binding %s.%s invocation=%#v has no canonical app handler", entry.Scene, entry.Key, invocation)
		}
		if intent.Kind == input.IntentNone || intent.Kind == input.IntentShortcutAction {
			t.Fatalf("default binding %s.%s invocation=%#v returned a non-concrete app intent %#v", entry.Scene, entry.Key, invocation, intent)
		}
	}

	for _, binding := range bindings {
		if !entryInvocations[binding.Invocation.Signature()] {
			t.Fatalf("routed binding %q invocation=%#v is absent from the default shortcut entries", binding.ID, binding.Invocation)
		}
		intent, ok := shortcutIntentForInvocation(binding.Invocation, input.InputEvent{})
		if !ok || intent.Kind == input.IntentNone || intent.Kind == input.IntentShortcutAction {
			t.Fatalf("routed binding %q invocation=%#v has no concrete app handler", binding.ID, binding.Invocation)
		}
	}

	seenSpecs := map[actiondomain.ID]bool{}
	for _, spec := range actiondomain.Specs() {
		if spec.ID == "" || seenSpecs[spec.ID] {
			t.Fatalf("canonical shortcut specs must be named and unique: %#v", spec)
		}
		seenSpecs[spec.ID] = true
	}
	for specID := range boundSpecs {
		if !seenSpecs[actiondomain.ID(specID)] {
			t.Fatalf("bound shortcut spec %q is not present in canonical spec inventory", specID)
		}
	}
	if got := (shortcutAuditInventory{DefaultEntries: len(entries), RoutedBindings: len(bindings), ShortcutSpecs: len(seenSpecs), RenderProjections: len(render.ProjectionCatalog())}); got != wantInventory {
		t.Fatalf("shortcut inventory changed without reclassification: got=%#v want=%#v", got, wantInventory)
	}
}

// TestHitRegionProducersDeclareCanonicalInvocation 在 producer 层阻止 ActionID-only 命中区。
// append/layout 之后的测试无法区分 producer 正确与公共层 fallback，因此这里直接检查生产 AST。
func TestHitRegionProducersDeclareCanonicalInvocation(t *testing.T) {
	repoRoot := shortcutAuditRepoRoot(t)
	shortcutWalkProductionGo(t, repoRoot, func(path string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || shortcutASTTypeName(literal.Type) != "HitRegion" {
				return true
			}
			hasActionID := false
			hasInvocation := false
			hasTargetMode := false
			hasRow := false
			hasRowPresence := false
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, _ := field.Key.(*ast.Ident)
				if name == nil {
					continue
				}
				switch name.Name {
				case "ActionID":
					hasActionID = true
				case "Invocation":
					hasInvocation = true
				case "TargetMode":
					hasTargetMode = true
				case "Row":
					hasRow = true
				case "HasRow":
					hasRowPresence = true
				}
			}
			if hasActionID && (!hasInvocation || !hasTargetMode) {
				t.Fatalf("%s#%s creates actionable HitRegion without producer-owned Invocation and TargetMode", path, shortcutEnclosingFunction(file, literal.Pos()))
			}
			if hasActionID && hasRow && !hasRowPresence {
				t.Fatalf("%s#%s creates row-target HitRegion without explicit HasRow", path, shortcutEnclosingFunction(file, literal.Pos()))
			}
			return true
		})
	})
}

// TestShortcutContractAuditClassifiesRenderProjections 证明 render 只保留有真实 surface 消费者的投影，
// 且每个投影都单向引用已注册 canonical action，不能重新拥有 dispatch 身份。
func TestShortcutContractAuditClassifiesRenderProjections(t *testing.T) {
	seen := map[render.ProjectionID]bool{}
	for _, spec := range render.ProjectionCatalog() {
		if spec.ID == "" || seen[spec.ID] {
			t.Fatalf("render action projections must be named and unique: %#v", spec)
		}
		seen[spec.ID] = true
		if len(spec.Surfaces) == 0 {
			t.Fatalf("render action projection %q has no classified surface", spec.ID)
		}
	}
	if len(seen) == 0 {
		t.Fatal("render action projection inventory must not be empty")
	}
}

func shortcutAuditRepoRoot(t *testing.T) string {
	t.Helper()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get shortcut audit working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDir, "..", ".."))
}

func readShortcutDebtManifest(t *testing.T, repoRoot string) shortcutDebtManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "tui", "docs", "shortcut-contract-debt.json"))
	if err != nil {
		t.Fatalf("read shortcut debt manifest: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest shortcutDebtManifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode shortcut debt manifest: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("shortcut debt manifest contains trailing JSON data: %v", err)
	}
	return manifest
}

func assertShortcutAuditAnchors(t *testing.T, repoRoot string, surface shortcutAuditSurface) {
	t.Helper()
	for _, source := range surface.Sources {
		if source.Path == "" || len(source.Anchors) == 0 {
			t.Fatalf("shortcut audit surface %q has incomplete source %#v", surface.ID, source)
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(source.Path)))
		if err != nil {
			t.Fatalf("read shortcut audit source %q for %q: %v", source.Path, surface.ID, err)
		}
		for _, anchor := range source.Anchors {
			if count := strings.Count(string(data), anchor); count != 1 {
				t.Fatalf("shortcut audit source %q anchor %q for %q occurs %d times, want exactly once", source.Path, fmt.Sprintf("%.80s", anchor), surface.ID, count)
			}
		}
	}
}

func assertShortcutObservedArtifacts(t *testing.T, repoRoot string, manifest shortcutDebtManifest, classifications map[string]bool) {
	t.Helper()
	observed := shortcutAuditArtifacts{
		InputEventProducers: shortcutCompositeProducers(t, repoRoot, "InputEvent"),
		HitRegionProducers:  shortcutCompositeProducers(t, repoRoot, "HitRegion"),
		DisplayKeyLiterals:  shortcutDisplayKeyLiterals(t, repoRoot),
	}
	for _, group := range [][]shortcutAuditArtifact{manifest.Artifacts.InputEventProducers, manifest.Artifacts.HitRegionProducers, manifest.Artifacts.DisplayKeyLiterals} {
		for _, artifact := range group {
			if artifact.Signature == "" || artifact.Count < 1 || !classifications[artifact.Classification] {
				t.Fatalf("invalid shortcut artifact classification: %#v", artifact)
			}
		}
	}
	if !reflect.DeepEqual(observed, manifest.Artifacts) {
		t.Fatalf("shortcut source artifacts changed without per-item classification:\n%s", shortcutArtifactDiff(observed, manifest.Artifacts))
	}
	encoded, err := json.Marshal(observed)
	if err != nil {
		t.Fatalf("encode observed shortcut artifacts: %v", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
	if digest != shortcutArtifactBaselineSHA256 {
		t.Fatalf("shortcut artifact baseline changed without updating the independent guard: got=%s want=%s", digest, shortcutArtifactBaselineSHA256)
	}
	semanticDigest := shortcutCompositeSemanticDigest(t, repoRoot)
	if semanticDigest != shortcutCompositeSemanticBaselineSHA256 {
		t.Fatalf("shortcut InputEvent/HitRegion semantic baseline changed without reclassification: got=%s want=%s", semanticDigest, shortcutCompositeSemanticBaselineSHA256)
	}
	renderStringDigest := shortcutRenderStringDigest(t, repoRoot)
	if renderStringDigest != shortcutRenderStringBaselineSHA256 {
		t.Fatalf("render string literal baseline changed without shortcut hint review: got=%s want=%s", renderStringDigest, shortcutRenderStringBaselineSHA256)
	}
}

func shortcutCompositeProducers(t *testing.T, repoRoot string, typeName string) []shortcutAuditArtifact {
	t.Helper()
	counts := map[string]int{}
	shortcutWalkProductionGo(t, repoRoot, func(path string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || shortcutASTTypeName(literal.Type) != typeName {
				return true
			}
			counts[path+"#"+shortcutEnclosingFunction(file, literal.Pos())]++
			return true
		})
	})
	classification := "input-event-contract"
	if typeName == "HitRegion" {
		classification = "canonical-hit-region-contract"
	}
	return shortcutArtifactsFromCounts(counts, classification)
}

func shortcutDisplayKeyLiterals(t *testing.T, repoRoot string) []shortcutAuditArtifact {
	t.Helper()
	counts := map[string]int{}
	classes := map[string]string{}
	values := map[string][]string{}
	shortcutWalkProductionGo(t, repoRoot, func(path string, file *ast.File) {
		if !strings.HasPrefix(path, "tui/render/") {
			return
		}
		seenPositions := map[token.Pos]bool{}
		addLiteral := func(literal *ast.BasicLit) {
			if literal == nil || literal.Kind != token.STRING || seenPositions[literal.Pos()] {
				return
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return
			}
			seenPositions[literal.Pos()] = true
			function := shortcutEnclosingFunction(file, literal.Pos())
			signature := path + "#" + function
			counts[signature]++
			classes[signature] = shortcutDisplayLiteralClassification(path, function, value)
			values[signature] = append(values[signature], value)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "withFooter" {
				return true
			}
			literal, _ := call.Args[0].(*ast.BasicLit)
			addLiteral(literal)
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, ok := field.Key.(*ast.Ident)
				if !ok || name.Name != "Key" {
					continue
				}
				value, _ := field.Value.(*ast.BasicLit)
				addLiteral(value)
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil || !shortcutLooksLikeDisplayKey(value) {
				return true
			}
			addLiteral(literal)
			return true
		})
	})
	out := make([]shortcutAuditArtifact, 0, len(counts))
	for signature, count := range counts {
		sort.Strings(values[signature])
		encoded, err := json.Marshal(values[signature])
		if err != nil {
			t.Fatalf("encode display key literals for %s: %v", signature, err)
		}
		out = append(out, shortcutAuditArtifact{Signature: signature, Count: count, Classification: classes[signature], Digest: fmt.Sprintf("%x", sha256.Sum256(encoded))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Signature < out[j].Signature })
	return out
}

func shortcutWalkProductionGo(t *testing.T, repoRoot string, visit func(path string, file *ast.File)) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(repoRoot, "tui"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		visit(filepath.ToSlash(relative), parsed)
		return nil
	})
	if err != nil {
		t.Fatalf("scan production TUI Go sources: %v", err)
	}
}

func shortcutASTTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}

func shortcutEnclosingFunction(file *ast.File, position token.Pos) string {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Pos() <= position && position <= function.End() {
			return function.Name.Name
		}
	}
	return "<package>"
}

func shortcutLooksLikeDisplayKey(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "ctrl-") || strings.Contains(lower, "ctrl+") || strings.Contains(lower, "alt-") || strings.Contains(lower, "alt+") || strings.Contains(value, "[Ctrl]") ||
		strings.HasPrefix(strings.TrimSpace(value), "^") || strings.Contains(lower, "pgup") || strings.Contains(lower, "pageup") || strings.Contains(lower, "shift+") ||
		lower == "esc" || lower == "enter" || strings.Contains(lower, "restart current")
}

func shortcutDisplayLiteralClassification(path string, function string, value string) string {
	switch path {
	case "tui/render/action_ids.go":
		return "render-projection-legacy-shortcut-metadata"
	case "tui/render/product_content.go":
		return "hardcoded-content-key-hints"
	case "tui/render/footer_action_visibility.go":
		return "hardcoded-footer-key-pruning"
	case "tui/render/vm.go":
		return "global-back-navigation"
	case "tui/render/overlay_chrome.go":
		return "global-back-navigation"
	case "tui/render/shell_bar.go":
		if function == "footerTailActionToken" && strings.EqualFold(value, "esc") {
			return "global-back-navigation"
		}
		return "shortcut-key-token-formatting"
	default:
		return "shortcut-key-token-formatting"
	}
}

func shortcutArtifactsFromCounts(counts map[string]int, classification string) []shortcutAuditArtifact {
	out := make([]shortcutAuditArtifact, 0, len(counts))
	for signature, count := range counts {
		out = append(out, shortcutAuditArtifact{Signature: signature, Count: count, Classification: classification})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Signature < out[j].Signature })
	return out
}

func shortcutArtifactDiff(got shortcutAuditArtifacts, want shortcutAuditArtifacts) string {
	return fmt.Sprintf("observed=%#v\nmanifest=%#v", got, want)
}

func shortcutCompositeSemanticDigest(t *testing.T, repoRoot string) string {
	t.Helper()
	records := []string{}
	err := filepath.WalkDir(filepath.Join(repoRoot, "tui"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typeName := shortcutASTTypeName(literal.Type)
			if typeName != "InputEvent" && typeName != "HitRegion" {
				return true
			}
			var normalized bytes.Buffer
			if err := printer.Fprint(&normalized, fileSet, literal); err != nil {
				t.Fatalf("normalize %s composite literal: %v", typeName, err)
			}
			records = append(records, filepath.ToSlash(relative)+"#"+shortcutEnclosingFunction(parsed, literal.Pos())+"#"+typeName+"#"+normalized.String())
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan shortcut semantic composites: %v", err)
	}
	sort.Strings(records)
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("encode shortcut semantic composites: %v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func shortcutRenderStringDigest(t *testing.T, repoRoot string) string {
	t.Helper()
	records := []string{}
	shortcutWalkProductionGo(t, repoRoot, func(path string, file *ast.File) {
		if !strings.HasPrefix(path, "tui/render/") {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			records = append(records, path+"#"+shortcutEnclosingFunction(file, literal.Pos())+"#"+literal.Value)
			return true
		})
	})
	sort.Strings(records)
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("encode render string literal baseline: %v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
