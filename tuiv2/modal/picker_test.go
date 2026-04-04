package modal

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/input"
)

// TestPickerStateFields 验证 PickerState 能存储终端列表、当前选中项、搜索 query。
func TestPickerStateFields(t *testing.T) {
	ps := PickerState{
		Items: []PickerItem{
			{TerminalID: "t1", Name: "bash", State: "running"},
			{TerminalID: "t2", Name: "vim", State: "running"},
		},
		Selected: 1,
		Query:    "vi",
	}

	if len(ps.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(ps.Items))
	}
	if ps.Items[0].TerminalID != "t1" {
		t.Errorf("unexpected item[0].TerminalID: %q", ps.Items[0].TerminalID)
	}
	if ps.Items[1].Name != "vim" {
		t.Errorf("unexpected item[1].Name: %q", ps.Items[1].Name)
	}
	if ps.Selected != 1 {
		t.Errorf("expected Selected=1, got %d", ps.Selected)
	}
	if ps.Query != "vi" {
		t.Errorf("expected Query=%q, got %q", "vi", ps.Query)
	}
}

// TestPickerItemState 验证 PickerItem 正确携带 State 字段。
func TestPickerItemState(t *testing.T) {
	item := PickerItem{TerminalID: "t3", Name: "htop", State: "exited"}
	if item.State != "exited" {
		t.Errorf("expected State=%q, got %q", "exited", item.State)
	}
}

// TestModalHostOpenPickerSession 验证 ModalHost.Open(ModePicker, requestID) 后
// Session 被正确初始化：Kind=ModePicker、Phase=ModalPhaseOpening、RequestID 匹配。
func TestModalHostOpenPickerSession(t *testing.T) {
	h := NewHost()
	h.Open(input.ModePicker, "req-abc")

	if h.Session == nil {
		t.Fatal("expected non-nil Session after Open")
	}
	if h.Session.Kind != input.ModePicker {
		t.Errorf("expected Kind=%q, got %q", input.ModePicker, h.Session.Kind)
	}
	if h.Session.Phase != ModalPhaseOpening {
		t.Errorf("expected Phase=%q, got %q", ModalPhaseOpening, h.Session.Phase)
	}
	if h.Session.RequestID != "req-abc" {
		t.Errorf("expected RequestID=%q, got %q", "req-abc", h.Session.RequestID)
	}
	if h.Session.Loading {
		t.Error("expected Loading=false after Open")
	}
}

// TestModalHostOpenReplacesExistingSession 验证重新 Open 会替换旧 Session。
func TestModalHostOpenReplacesExistingSession(t *testing.T) {
	h := NewHost()
	h.Open(input.ModePicker, "req-1")
	h.Open(input.ModePicker, "req-2")

	if h.Session.RequestID != "req-2" {
		t.Errorf("expected RequestID=%q after second Open, got %q", "req-2", h.Session.RequestID)
	}
}

// TestModalHostCloseNilSafe 验证对 nil host 调用 Close 不会 panic。
func TestModalHostCloseNilSafe(t *testing.T) {
	var h *ModalHost
	h.Close(input.ModePicker, "req-1") // must not panic
}

// TestPickerStateZeroValue 验证 PickerState 零值：Items 为 nil、Selected=0、Query=""。
func TestPickerStateZeroValue(t *testing.T) {
	var ps PickerState
	if ps.Items != nil {
		t.Errorf("expected nil Items in zero value, got %v", ps.Items)
	}
	if ps.Selected != 0 {
		t.Errorf("expected Selected=0, got %d", ps.Selected)
	}
	if ps.Query != "" {
		t.Errorf("expected empty Query, got %q", ps.Query)
	}
}

func TestPickerStateVisibleItemsKeepsCreateRowFirst(t *testing.T) {
	ps := PickerState{
		Items: []PickerItem{
			{TerminalID: "term-1", Name: "shell"},
			{CreateNew: true, Name: "new terminal"},
			{TerminalID: "term-2", Name: "worker"},
		},
	}
	items := ps.VisibleItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 visible items, got %d", len(items))
	}
	if !items[0].CreateNew {
		t.Fatalf("expected create row to be first, got %#v", items)
	}
	if items[1].TerminalID != "term-1" || items[2].TerminalID != "term-2" {
		t.Fatalf("expected non-create items to preserve order, got %#v", items)
	}
}
