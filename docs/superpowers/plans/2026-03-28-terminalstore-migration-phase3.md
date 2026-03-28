# TerminalStore Migration Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce `TerminalStore` and the TUI-local `Terminal` proxy object so terminal identity, metadata, and shared read paths begin to live on formal terminal objects rather than being scattered across `Pane`, `Viewport`, and `Model`.

**Architecture:** This phase is a migration, not a rewrite. We will first establish `TerminalStore` as the single in-memory registry for TUI-side terminal proxies, then introduce a heavier `Terminal` model and begin wiring `Pane -> *Terminal` relationships, while deliberately leaving `TerminalCoordinator`, `Resizer`, renderer redesign, and full runtime coordination for later phases. The transition must preserve current runtime behavior by migrating ownership and read paths incrementally rather than clearing all legacy fields at once.

**Tech Stack:** Go, Bubble Tea, existing `tui/` package, Go tests

---

## File map

### Existing files to modify
- `tui/model.go`
  - Current `Pane`, `Viewport`, `Model`, and assorted terminal-related fields
  - Will start integrating `TerminalStore` and `Pane -> *Terminal`
- `tui/picker.go`
  - Terminal picker reads current terminal identity / metadata from pane-local fields
  - Will begin reading through `*Terminal` where safe
- `tui/render.go`
  - Reads pane title / terminal status from pane-local data
  - Will start consuming terminal-backed read paths where practical
- `tui/workbench.go`
  - Workbench owns the workspace tree and pane objects
  - May need small integration hooks for terminal-backed pane state
- `tui/model_test.go`
  - Existing broad TUI integration coverage
  - Add/adjust tests for `Pane -> *Terminal` and terminal-backed behavior
- `tui/picker_test.go` or existing terminal-picker tests in `tui/model_test.go`
  - Extend tests to ensure picker paths still work with terminal-backed state
- `tui/render_benchmark_test.go`
  - May need small updates if pane title/status reads shift through terminal-backed paths

### New files to create
- `tui/terminal_store.go`
  - `TerminalStore` type
  - register / get / list / delete helpers
- `tui/terminal_model.go`
  - `Terminal` proxy type
  - terminal identity / metadata / state / runtime-mirror fields
- `tui/terminal_store_test.go`
  - Focused tests for registry behavior
- `tui/terminal_model_test.go`
  - Focused tests for terminal object semantics

### Boundaries to preserve in Phase 3
- Do **not** introduce `TerminalCoordinator`
- Do **not** introduce `Resizer`
- Do **not** redesign renderer or render loop
- Do **not** migrate full stream / attach / snapshot / recovery coordination into the new types
- Do **not** remove all legacy pane/viewport terminal fields in one pass
- Do **not** refactor shared-terminal ownership architecture beyond moving object/field home where necessary

---

## Task 1: Introduce `TerminalStore` as the unique terminal registry

**Files:**
- Create: `tui/terminal_store.go`
- Create: `tui/terminal_store_test.go`
- Modify: `tui/model.go`

- [ ] **Step 1: Write the failing `TerminalStore` tests**

```go
func TestTerminalStoreReturnsSameTerminalForSameID(t *testing.T) {
	store := NewTerminalStore()

	first := store.GetOrCreate("term-1")
	second := store.GetOrCreate("term-1")

	if first == nil || second == nil {
		t.Fatal("expected terminal objects")
	}
	if first != second {
		t.Fatal("expected same pointer for same terminal id")
	}
}

func TestTerminalStoreDeleteRemovesTerminal(t *testing.T) {
	store := NewTerminalStore()
	store.GetOrCreate("term-1")

	store.Delete("term-1")

	if store.Get("term-1") != nil {
		t.Fatal("expected terminal to be removed from store")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalStore(ReturnsSameTerminalForSameID|DeleteRemovesTerminal)' -count=1`
Expected: FAIL with undefined `NewTerminalStore`, `GetOrCreate`, or `Delete`.

- [ ] **Step 3: Add the minimal `TerminalStore` type**

Create `tui/terminal_store.go`:

```go
package tui

type TerminalStore struct {
	items map[string]*Terminal
}

func NewTerminalStore() *TerminalStore {
	return &TerminalStore{items: make(map[string]*Terminal)}
}

func (s *TerminalStore) Get(id string) *Terminal {
	if s == nil || id == "" {
		return nil
	}
	return s.items[id]
}

func (s *TerminalStore) GetOrCreate(id string) *Terminal {
	if s == nil || id == "" {
		return nil
	}
	if terminal := s.items[id]; terminal != nil {
		return terminal
	}
	terminal := &Terminal{ID: id}
	s.items[id] = terminal
	return terminal
}

func (s *TerminalStore) Delete(id string) {
	if s == nil || id == "" {
		return
	}
	delete(s.items, id)
}

func (s *TerminalStore) List() []*Terminal {
	if s == nil {
		return nil
	}
	items := make([]*Terminal, 0, len(s.items))
	for _, terminal := range s.items {
		if terminal != nil {
			items = append(items, terminal)
		}
	}
	return items
}
```

Modify `tui/model.go` to add a field:

```go
terminalStore *TerminalStore
```

And initialize it in `NewModel(...)`:

```go
terminalStore := NewTerminalStore()
```

Then place it into the model literal:

```go
terminalStore: terminalStore,
```

- [ ] **Step 4: Run focused `TerminalStore` tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalStore(ReturnsSameTerminalForSameID|DeleteRemovesTerminal)' -count=1`
Expected: PASS.

---

## Task 2: Introduce the TUI-local `Terminal` proxy model

**Files:**
- Create: `tui/terminal_model.go`
- Create: `tui/terminal_model_test.go`
- Modify: `tui/terminal_store.go`

- [ ] **Step 1: Write the failing `Terminal` model tests**

```go
func TestTerminalProxyStoresIdentityAndMetadata(t *testing.T) {
	terminal := &Terminal{ID: "term-1"}
	terminal.SetMetadata("worker", []string{"tail", "-f", "worker.log"}, map[string]string{"role": "worker"})

	if terminal.Name != "worker" {
		t.Fatalf("expected terminal name worker, got %q", terminal.Name)
	}
	if len(terminal.Command) != 3 || terminal.Command[0] != "tail" {
		t.Fatalf("expected command to be stored, got %v", terminal.Command)
	}
	if terminal.Tags["role"] != "worker" {
		t.Fatalf("expected role tag worker, got %q", terminal.Tags["role"])
	}
}

func TestTerminalProxyClonesMetadataInput(t *testing.T) {
	command := []string{"bash"}
	tags := map[string]string{"role": "dev"}
	terminal := &Terminal{ID: "term-1"}
	terminal.SetMetadata("shell", command, tags)

	command[0] = "zsh"
	tags["role"] = "ops"

	if terminal.Command[0] != "bash" {
		t.Fatalf("expected terminal command clone, got %v", terminal.Command)
	}
	if terminal.Tags["role"] != "dev" {
		t.Fatalf("expected terminal tag clone, got %v", terminal.Tags)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalProxy(StoresIdentityAndMetadata|ClonesMetadataInput)' -count=1`
Expected: FAIL with undefined `Terminal` or `SetMetadata`.

- [ ] **Step 3: Add the minimal `Terminal` model**

Create `tui/terminal_model.go`:

```go
package tui

import "github.com/lozzow/termx/protocol"

type Terminal struct {
	ID        string
	Name      string
	Command   []string
	Tags      map[string]string
	State     string
	ExitCode  *int
	Snapshot  *protocol.Snapshot
	Channel   uint16
	AttachMode string
}

func (t *Terminal) SetMetadata(name string, command []string, tags map[string]string) {
	if t == nil {
		return
	}
	t.Name = name
	t.Command = append([]string(nil), command...)
	t.Tags = cloneStringMap(tags)
}
```

- [ ] **Step 4: Run focused `Terminal` model tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalProxy(StoresIdentityAndMetadata|ClonesMetadataInput)' -count=1`
Expected: PASS.

---

## Task 3: Start `Pane -> *Terminal` relationship

**Files:**
- Modify: `tui/model.go`
- Modify: `tui/terminal_model.go`
- Modify: `tui/model_test.go`

- [ ] **Step 1: Write the failing `Pane -> *Terminal` test**

```go
func TestPaneCanReferenceSharedTerminalObject(t *testing.T) {
	store := NewTerminalStore()
	terminal := store.GetOrCreate("term-1")
	terminal.SetMetadata("shared", []string{"bash"}, map[string]string{"role": "dev"})

	first := &Pane{ID: "pane-1", Terminal: terminal, Viewport: &Viewport{}}
	second := &Pane{ID: "pane-2", Terminal: terminal, Viewport: &Viewport{}}

	terminal.Name = "renamed"

	if first.Terminal.Name != "renamed" || second.Terminal.Name != "renamed" {
		t.Fatal("expected panes to share terminal object reference")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestPaneCanReferenceSharedTerminalObject' -count=1`
Expected: FAIL because `Pane` does not yet have `Terminal *Terminal`.

- [ ] **Step 3: Add `Terminal *Terminal` to `Pane`**

Modify the `Pane` type in `tui/model.go`:

```go
type Pane struct {
	ID       string
	Title    string
	Terminal *Terminal
	*Viewport
}
```

- [ ] **Step 4: Add a minimal terminal-sync helper for panes**

In `tui/model.go`, add a helper that binds pane-local identity / metadata into the terminal object when available:

```go
func (m *Model) ensurePaneTerminal(pane *Pane) *Terminal {
	if m == nil || m.terminalStore == nil || pane == nil {
		return nil
	}
	if pane.Terminal != nil {
		return pane.Terminal
	}
	terminalID := strings.TrimSpace(pane.TerminalID)
	if terminalID == "" {
		return nil
	}
	terminal := m.terminalStore.GetOrCreate(terminalID)
	terminal.SetMetadata(pane.Name, pane.Command, pane.Tags)
	terminal.State = pane.TerminalState
	terminal.ExitCode = pane.ExitCode
	terminal.Snapshot = pane.Snapshot
	terminal.Channel = pane.Channel
	terminal.AttachMode = pane.AttachMode
	pane.Terminal = terminal
	return terminal
}
```

- [ ] **Step 5: Run focused `Pane -> *Terminal` test**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestPaneCanReferenceSharedTerminalObject' -count=1`
Expected: PASS.

---

## Task 4: Migrate selected terminal read paths to `Terminal`

**Files:**
- Modify: `tui/picker.go`
- Modify: `tui/render.go`
- Modify: `tui/model.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Write failing read-path regression tests**

Add to `tui/model_test.go`:

```go
func TestPaneTitleCanReadFromTerminalObject(t *testing.T) {
	terminal := &Terminal{ID: "term-1", Name: "worker", Command: []string{"tail", "-f", "worker.log"}}
	pane := &Pane{ID: "pane-1", Terminal: terminal, Viewport: &Viewport{TerminalID: "term-1"}}

	if got := paneTitle(pane); got == "" {
		t.Fatal("expected pane title from terminal-backed pane")
	}
}

func TestTerminalPickerLocationsCanUseSharedTerminalMetadata(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	terminal := model.terminalStore.GetOrCreate("term-1")
	terminal.SetMetadata("worker", []string{"bash"}, map[string]string{"role": "dev"})
	pane := &Pane{ID: "pane-1", Title: "worker", Terminal: terminal, Viewport: &Viewport{TerminalID: "term-1"}}
	tab := &Tab{Name: "1", Panes: map[string]*Pane{pane.ID: pane}, ActivePaneID: pane.ID}
	model.workspace = Workspace{Name: "main", Tabs: []*Tab{tab}, ActiveTab: 0}

	locations := model.terminalLocations()
	if len(locations["term-1"]) == 0 {
		t.Fatal("expected location for terminal-backed pane")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'Test(PaneTitleCanReadFromTerminalObject|TerminalPickerLocationsCanUseSharedTerminalMetadata)' -count=1`
Expected: FAIL because read paths still assume pane-local terminal data.

- [ ] **Step 3: Add terminal-backed read fallbacks**

In `tui/model.go`, update terminal-facing pane reads to prefer `pane.Terminal` when available. Example for pane title helper:

```go
func paneTitle(pane *Pane) string {
	if pane == nil {
		return ""
	}
	if pane.Terminal != nil {
		name := strings.TrimSpace(pane.Terminal.Name)
		if name != "" {
			return name
		}
	}
	// existing fallback logic remains
	...
}
```

In `tui/picker.go`, before reading terminal-facing pane metadata, ensure panes are terminal-backed:

```go
for _, pane := range tab.Panes {
	if pane == nil || pane.TerminalID == "" {
		continue
	}
	m.ensurePaneTerminal(pane)
	...
}
```

In `tui/render.go`, only replace narrow terminal-facing title/meta reads where practical; do not redesign renderer internals.

- [ ] **Step 4: Run focused terminal-backed read tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'Test(PaneTitleCanReadFromTerminalObject|TerminalPickerLocationsCanUseSharedTerminalMetadata)' -count=1`
Expected: PASS.

---

## Task 5: Verify Phase 3 end-to-end

**Files:**
- Test: `tui/terminal_store_test.go`
- Test: `tui/terminal_model_test.go`
- Test: `tui/model_test.go`

- [ ] **Step 1: Run focused `TerminalStore + Terminal` migration regressions**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run 'TestTerminalStore|TestTerminalProxy|TestPaneCanReferenceSharedTerminalObject|Test(PaneTitleCanReadFromTerminalObject|TerminalPickerLocationsCanUseSharedTerminalMetadata)' -count=1`
Expected: PASS.

- [ ] **Step 2: Run full TUI package tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -count=1`
Expected: PASS.

- [ ] **Step 3: Run full repository tests**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`
Expected: PASS.

- [ ] **Step 4: Build the CLI**

Run: `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go build ./cmd/termx`
Expected: build succeeds.

- [ ] **Step 5: Commit the completed phase**

```bash
git add tui/terminal_store.go tui/terminal_model.go tui/terminal_store_test.go tui/terminal_model_test.go tui/model.go tui/model_test.go tui/picker.go tui/render.go
git commit -m "refactor: 引入 TerminalStore 与 Terminal 代理对象"
```

---

## Notes for the implementing engineer

### Phase 3 success criteria
- `TerminalStore` is a real object, not a temporary map
- `Terminal` is a real object, not a field bag spread across `Pane`/`Viewport`
- `Pane -> *Terminal` starts to hold
- A batch of terminal read paths trusts `Terminal`
- `TerminalPoolPage` now has a clear future shared data source
- Runtime coordinator concerns remain deferred

### Explicit non-goals
- No `TerminalCoordinator`
- No `Resizer`
- No renderer/render-loop redesign
- No full stream / attach / recovery migration
- No one-pass deletion of all pane/viewport terminal legacy fields

---

## Self-review

### Spec coverage
- `TerminalStore` introduction covered by Task 1
- `Terminal` proxy introduction covered by Task 2
- `Pane -> *Terminal` relation covered by Task 3
- Terminal-backed read-path migration covered by Task 4
- Phase-wide verification covered by Task 5

### Placeholder scan
- No `TODO` / `TBD`
- Every task has concrete files, code, and commands
- Coordinator/runtime migration is explicitly deferred

### Type consistency
- `TerminalStore`, `Terminal`, `GetOrCreate`, `SetMetadata`, `TerminalPickerContext`, `ensurePaneTerminal`, and `Pane.Terminal` are named consistently across tasks

---

Plan complete and saved to `docs/superpowers/plans/2026-03-28-terminalstore-migration-phase3.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
