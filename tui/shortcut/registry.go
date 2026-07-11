// Package shortcut owns the TUI shortcut action domain contract.
// It has no dependency on config, input, render, or app packages so every
// consumer projects the same action identity, parameters, and display policy.
package shortcut

import (
	"fmt"
	"strconv"
	"strings"
)

// Visibility explicitly declares whether an action participates in a surface.
type Visibility string

const (
	VisibilityHidden  Visibility = "hidden"
	VisibilityVisible Visibility = "visible"
)

// ClickPolicy explicitly declares whether a rendered item can dispatch.
type ClickPolicy string

const (
	ClickHidden    ClickPolicy = "hidden"
	ClickHintOnly  ClickPolicy = "hint-only"
	ClickClickable ClickPolicy = "clickable"
)

// DisplayPolicy declares footer/help visibility and click behavior.
// Aggregated items containing multiple invocations must be projected as
// hint-only by the renderer even when the individual action is clickable.
type DisplayPolicy struct {
	Footer Visibility
	Help   Visibility
	Click  ClickPolicy
}

// ParamSpec describes one canonical action parameter.
type ParamSpec struct {
	Name string
	Min  int
	Max  int
}

// ActionSpec is the shortcut domain truth for one canonical action ID.
// Execution handlers belong to app; this type only owns identity, validation,
// scene capability, default wording, and display policy.
type ActionSpec struct {
	ID            string
	Aliases       []string
	AllowedScenes []string
	DefaultLabel  string
	Param         *ParamSpec
	Display       DisplayPolicy
	Routable      bool
}

// ActionInvocation is the canonical action sent by keyboard and click paths.
// SourceActionID is diagnostic input only and must never select execution.
type ActionInvocation struct {
	ID             string
	Params         map[string]int
	SourceActionID string
}

// Signature 返回可稳定比较的 invocation 身份。
// shortcut domain 用它判断多个展示项是否仍指向同一个 action 与参数；当前 canonical 参数只有 index，
// 新增参数时必须同步扩展这里，避免 renderer 把语义不同的动作合并成可点击提示。
func (invocation ActionInvocation) Signature() string {
	parts := []string{invocation.ID}
	if invocation.Params != nil {
		if value, ok := invocation.Params["index"]; ok {
			parts = append(parts, "index="+strconv.Itoa(value))
		}
	}
	return strings.Join(parts, "\x00")
}

// Param returns a canonical integer parameter.
func (invocation ActionInvocation) Param(name string) (int, bool) {
	value, ok := invocation.Params[name]
	return value, ok
}

var displayAction = DisplayPolicy{Footer: VisibilityVisible, Help: VisibilityVisible, Click: ClickClickable}
var displayHelpHint = DisplayPolicy{Footer: VisibilityHidden, Help: VisibilityVisible, Click: ClickHintOnly}

var defaultLabels = map[string]string{
	"menu.panel":                     "PANE",
	"menu.resize":                    "RESIZE",
	"menu.system":                    "GLOBAL",
	"menu.floating":                  "FLOAT",
	"menu.tab":                       "TAB",
	"menu.workspace":                 "WORKSPACE",
	"terminal_picker.open":           "PICKER",
	"copy.enter":                     "COPY",
	"panel.close":                    "CLOSE",
	"panel.detach":                   "DETACH",
	"panel.take_owner":               "OWNER",
	"panel.size_lock":                "LOCK",
	"panel.split_right":              "VSPLIT",
	"panel.split_down":               "HSPLIT",
	"panel.toggle_zoom":              "ZOOM",
	"panel.balance":                  "BALANCE",
	"panel.presentation_card":        "CARD",
	"panel.presentation_split_line":  "LINE",
	"panel.focus_next":               "FOCUS",
	"panel.focus_prev":               "FOCUS",
	"resize.left":                    "resize left",
	"resize.right":                   "resize right",
	"resize.up":                      "resize up",
	"resize.down":                    "resize down",
	"resize.layout_toggle":           "LAYOUT",
	"resize.pan_left":                "PAN",
	"resize.pan_down":                "PAN",
	"resize.pan_up":                  "PAN",
	"resize.pan_right":               "PAN",
	"resize.align_left":              "ALIGN",
	"resize.align_right":             "ALIGN",
	"resize.align_top":               "ALIGN",
	"resize.align_bottom":            "ALIGN",
	"resize.center_x":                "CENTER",
	"resize.center_y":                "CENTER",
	"resize.center":                  "CENTER",
	"resize.layout_reset":            "RESET",
	"system.toggle_header":           "HEADER",
	"system.toggle_footer":           "FOOTER",
	"system.clear_toasts":            "CLEAR",
	"system.close_toast":             "TOAST",
	"system.open_terminal_pool":      "TERMINALS",
	"system.open_workbench_tree":     "TREE",
	"system.toggle_shortcut_lock":    "KEYLOCK",
	"system.open_prompt":             "PROMPT",
	"system.open_help":               "HELP",
	"system.quit":                    "QUIT",
	"floating.new":                   "NEW FLOAT",
	"floating.overview":              "OVERVIEW",
	"system.open_terminal_picker":    "PICK",
	"floating.take_owner":            "OWNER",
	"floating.collapse":              "HIDE",
	"floating.toggle_all":            "ALL",
	"floating.auto_fit":              "AUTO-FIT",
	"floating.summon":                "SUMMON",
	"floating.close":                 "CLOSE",
	"floating.center":                "CENTER",
	"floating.fit":                   "FIT",
	"tab.create":                     "NEW",
	"tab.next":                       "NEXT",
	"tab.previous":                   "PREV",
	"tab.rename":                     "RENAME",
	"tab.close":                      "CLOSE",
	"tab.kill":                       "CLOSE",
	"workspace.create":               "NEW",
	"workspace.next":                 "NEXT",
	"workspace.previous":             "PREV",
	"workspace.rename":               "RENAME",
	"workspace.delete":               "DELETE",
	"copy.request_older":             "SCROLL",
	"copy.open_clipboard_history":    "CLIPBOARD",
	"terminal_pool.attach_tab":       "TAB",
	"terminal_pool.attach_float":     "FLOAT",
	"terminal_pool.attach":           "ATTACH",
	"terminal_pool.restart":          "RESTART",
	"terminal_pool.edit":             "RENAME",
	"terminal_pool.kill":             "KILL",
	"terminal_pool.delete":           "REMOVE",
	"floating_overview.open":         "OPEN",
	"floating_overview.show_all":     "SHOW ALL",
	"floating_overview.collapse_all": "COLLAPSE ALL",
	"clipboard_history.paste":        "PASTE",
	"clipboard_history.new":          "NEW",
	"clipboard_history.edit":         "EDIT",
	"clipboard_history.delete":       "DELETE",
}

var specs = buildSpecs()

func buildSpecs() map[string]ActionSpec {
	out := map[string]ActionSpec{}
	add := func(spec ActionSpec) {
		if spec.DefaultLabel == "" {
			if label := defaultLabels[spec.ID]; label != "" {
				spec.DefaultLabel = label
			} else {
				labelID := spec.ID
				if _, suffix, ok := strings.Cut(labelID, "."); ok {
					labelID = suffix
				}
				spec.DefaultLabel = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(labelID)
			}
		}
		if spec.Display.Footer == "" || spec.Display.Help == "" || spec.Display.Click == "" {
			panic("shortcut action " + spec.ID + " has incomplete display policy")
		}
		out[spec.ID] = spec
		for _, alias := range spec.Aliases {
			out[alias] = spec
		}
	}
	for _, scene := range []string{"panel", "resize", "system", "floating", "tab", "workspace", "copy", "terminal_picker", "terminal_pool", "workbench_tree", "clipboard_history", "floating_overview", "prompt", "help"} {
		aliases := []string(nil)
		if scene == "panel" {
			aliases = []string{"menu.pane"}
		}
		add(ActionSpec{ID: "menu." + scene, Aliases: aliases, AllowedScenes: routedScenes(), Display: displayAction, Routable: true})
	}
	add(ActionSpec{ID: "terminal_picker.open", Aliases: []string{"picker.open"}, AllowedScenes: routedScenes(), Display: displayAction, Routable: true})
	add(ActionSpec{ID: "copy.enter", AllowedScenes: []string{"global"}, Display: displayAction, Routable: true})
	addFixed := func(scene string, routable bool, ids ...string) {
		for _, id := range ids {
			add(ActionSpec{ID: id, AllowedScenes: []string{scene}, Display: displayAction, Routable: routable})
		}
	}
	addFixed("copy", true, "copy.request_older", "copy.request_newer", "copy.open_clipboard_history", "copy.paste_latest", "copy.paste_system")
	for _, id := range []string{"copy.line_start", "copy.line_end", "copy.cursor_left", "copy.cursor_right", "copy.cursor_down", "copy.cursor_up", "copy.accept", "copy.oldest", "copy.newest", "copy.half_page_older", "copy.half_page_newer", "copy.mark", "copy.copy_selection", "copy.search_start"} {
		add(ActionSpec{ID: id, AllowedScenes: []string{"copy"}, Display: displayHelpHint, Routable: true})
	}
	panel := []string{"panel.close", "panel.detach", "panel.reconnect", "panel.restart", "panel.take_owner", "panel.size_lock", "panel.split_right", "panel.split_down", "panel.kill", "panel.kill_and_close", "panel.toggle_zoom", "panel.balance", "panel.presentation_card", "panel.presentation_split_line", "panel.focus_next", "panel.focus_prev"}
	for _, id := range panel {
		alias := "pane." + strings.TrimPrefix(id, "panel.")
		allowed := []string{"panel"}
		if id == "panel.take_owner" || id == "panel.size_lock" || id == "panel.balance" {
			allowed = []string{"panel", "resize"}
		}
		add(ActionSpec{ID: id, Aliases: []string{alias}, AllowedScenes: allowed, Display: displayAction, Routable: true})
	}
	addFixed("resize", true, "resize.left", "resize.right", "resize.up", "resize.down", "resize.left_large", "resize.right_large", "resize.up_large", "resize.down_large", "resize.layout_toggle", "resize.pan_left", "resize.pan_right", "resize.pan_up", "resize.pan_down", "resize.align_left", "resize.align_right", "resize.align_top", "resize.align_bottom", "resize.center", "resize.center_x", "resize.center_y", "resize.layout_reset")
	addFixed("system", true, "system.toggle_header", "system.toggle_footer", "system.clear_toasts", "system.close_toast", "system.open_terminal_pool", "system.open_prompt", "system.open_help", "system.toggle_shortcut_lock", "system.quit")
	add(ActionSpec{ID: "system.open_terminal_picker", AllowedScenes: []string{"system", "floating"}, Display: displayAction, Routable: true})
	add(ActionSpec{ID: "system.open_workbench_tree", AllowedScenes: []string{"system", "workspace"}, Display: displayAction, Routable: true})
	addFixed("floating", true, "floating.new", "floating.overview", "floating.take_owner", "floating.collapse", "floating.center", "floating.toggle_all", "floating.fit", "floating.auto_fit", "floating.move_left", "floating.move_right", "floating.move_up", "floating.move_down", "floating.narrow", "floating.wide", "floating.short", "floating.tall")
	add(ActionSpec{ID: "floating.close", AllowedScenes: []string{"floating", "floating_overview"}, Display: displayAction, Routable: true})
	addFixed("tab", true, "tab.create", "tab.next", "tab.previous", "tab.rename", "tab.close", "tab.kill")
	add(ActionSpec{ID: "tab.jump", AllowedScenes: []string{"global", "tab"}, Param: &ParamSpec{Name: "index", Min: 1, Max: 9}, Display: displayAction, Routable: true})
	add(ActionSpec{ID: "floating.summon", AllowedScenes: []string{"floating", "floating_overview"}, Param: &ParamSpec{Name: "index", Min: 1, Max: 9}, Display: displayAction, Routable: true})
	addFixed("workspace", true, "workspace.create", "workspace.next", "workspace.previous", "workspace.rename", "workspace.delete")
	addFixed("terminal_picker", false, "terminal_picker.attach", "terminal_picker.split", "terminal_picker.edit", "terminal_picker.kill", "terminal_picker.delete", "terminal_picker.close")
	addFixed("terminal_pool", false, "terminal_pool.attach", "terminal_pool.attach_tab", "terminal_pool.attach_float", "terminal_pool.restart", "terminal_pool.edit", "terminal_pool.kill", "terminal_pool.delete", "terminal_pool.close")
	addFixed("workbench_tree", false, "workbench_tree.open", "workbench_tree.new", "workbench_tree.rename", "workbench_tree.delete", "workbench_tree.detach", "workbench_tree.zoom", "workbench_tree.close")
	addFixed("clipboard_history", false, "clipboard_history.paste", "clipboard_history.new", "clipboard_history.edit", "clipboard_history.delete", "clipboard_history.close")
	addFixed("floating_overview", false, "floating_overview.open", "floating_overview.show_all", "floating_overview.collapse_all", "floating_overview.close")
	addFixed("prompt", false, "prompt.submit", "prompt.cancel")
	addFixed("help", false, "help.close")
	return out
}

func routedScenes() []string {
	return []string{"global", "panel", "resize", "system", "floating", "tab", "workspace", "copy"}
}

// AllowsScene reports whether a canonical spec permits a binding in scene.
func (spec ActionSpec) AllowsScene(scene string) bool {
	if scene == "pane" {
		scene = "panel"
	}
	for _, allowed := range spec.AllowedScenes {
		if allowed == scene {
			return true
		}
	}
	return false
}

// ParseInvocation canonicalizes a configured action reference.
func ParseInvocation(source string) (ActionInvocation, ActionSpec, error) {
	source = strings.TrimSpace(source)
	base, paramText, parameterized := splitParameterizedAction(source)
	spec, ok := specs[base]
	if !ok {
		return ActionInvocation{}, ActionSpec{}, fmt.Errorf("unknown shortcut action %q", source)
	}
	invocation := ActionInvocation{ID: spec.ID, SourceActionID: source}
	if spec.Param == nil {
		if parameterized {
			return ActionInvocation{}, ActionSpec{}, fmt.Errorf("shortcut action %q does not accept a parameter", source)
		}
		return invocation, spec, nil
	}
	if !parameterized {
		return ActionInvocation{}, ActionSpec{}, fmt.Errorf("shortcut action %q requires %s", source, spec.Param.Name)
	}
	value, err := strconv.Atoi(paramText)
	if err != nil || value < spec.Param.Min || value > spec.Param.Max {
		return ActionInvocation{}, ActionSpec{}, fmt.Errorf("shortcut action %q has invalid %s", source, spec.Param.Name)
	}
	invocation.Params = map[string]int{spec.Param.Name: value}
	return invocation, spec, nil
}

func splitParameterizedAction(source string) (string, string, bool) {
	for _, base := range []string{"tab.jump", "floating.summon"} {
		prefix := base + "."
		if strings.HasPrefix(source, prefix) {
			return base, strings.TrimPrefix(source, prefix), true
		}
	}
	return source, "", false
}

// SpecForSource resolves a configured action reference to its canonical spec.
func SpecForSource(source string) (ActionSpec, bool) {
	_, spec, err := ParseInvocation(source)
	return spec, err == nil
}

// Specs returns each canonical action spec exactly once.
func Specs() []ActionSpec {
	seen := map[string]bool{}
	out := []ActionSpec{}
	for _, spec := range specs {
		if seen[spec.ID] {
			continue
		}
		seen[spec.ID] = true
		out = append(out, spec)
	}
	return out
}
