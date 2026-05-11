package input

import "testing"

func TestHelpSectionsSeparateResizeDisplayFromCopyMode(t *testing.T) {
	sections := HelpSections()
	resize := helpSectionByTitle(sections, "Resize & Display")
	copyMode := helpSectionByTitle(sections, "Copy Mode")
	if resize == nil {
		t.Fatal("expected Resize & Display help section")
	}
	if copyMode == nil {
		t.Fatal("expected Copy Mode help section")
	}

	assertHelpBinding(t, resize, "Shift-W/A/S/D or Shift-arrows", "pan terminal content inside pane")
	assertHelpBinding(t, resize, "0/$ ^/B", "align terminal content to pane edges")
	assertHelpBinding(t, copyMode, "Ctrl-V", "enter copy mode")
	assertHelpBinding(t, copyMode, "arrows or h/j/k/l", "move copy cursor")
	assertHelpBinding(t, copyMode, "H", "open clipboard history")
	assertHelpBindingOmitted(t, copyMode, "Shift-W/A/S/D or Shift-arrows", "pan terminal content inside pane")
}

func helpSectionByTitle(sections []HelpSectionDoc, title string) *HelpSectionDoc {
	for i := range sections {
		if sections[i].Title == title {
			return &sections[i]
		}
	}
	return nil
}

func assertHelpBinding(t *testing.T, section *HelpSectionDoc, key, action string) {
	t.Helper()
	if section == nil {
		t.Fatalf("nil help section for binding %q -> %q", key, action)
	}
	for _, binding := range section.Bindings {
		if binding.Key == key && binding.Action == action {
			return
		}
	}
	t.Fatalf("expected help binding %q -> %q in %#v", key, action, section.Bindings)
}

func assertHelpBindingOmitted(t *testing.T, section *HelpSectionDoc, key, action string) {
	t.Helper()
	if section == nil {
		return
	}
	for _, binding := range section.Bindings {
		if binding.Key == key && binding.Action == action {
			t.Fatalf("did not expect help binding %q -> %q in %#v", key, action, section.Bindings)
		}
	}
}
