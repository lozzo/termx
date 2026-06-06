package protocol

import "testing"

func TestWorkbenchPayloadRoundTrip(t *testing.T) {
	params := WorkbenchMutateParams{
		Action:          WorkbenchMutationPaneSplit,
		WorkspaceID:     "workspace-main",
		TabID:           "tab-main",
		PaneID:          "pane-main",
		TargetID:        "pane-two",
		Name:            "logs",
		Kind:            WorkbenchPaneTerminalLive,
		TerminalID:      "terminal-1",
		SplitDirection:  WorkbenchSplitVertical,
		CheckVersion:    true,
		ExpectedVersion: 7,
	}
	payload, err := EncodeMethodParams("workbench.apply", params)
	if err != nil {
		t.Fatalf("encode params: %v", err)
	}
	decoded, err := DecodeMethodParams("workbench.apply", payload)
	if err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if decoded.(WorkbenchMutateParams) != params {
		t.Fatalf("params roundtrip mismatch: %+v", decoded)
	}

	snapshot := WorkbenchSnapshot{
		Version:           8,
		ActiveWorkspaceID: "workspace-main",
		Workspaces: []WorkbenchWorkspace{{
			ID:          "workspace-main",
			Name:        "main",
			ActiveTabID: "tab-main",
			Tabs: []WorkbenchTab{{
				ID:           "tab-main",
				Title:        "shell",
				ActivePaneID: "pane-two",
				Panes: []WorkbenchPane{
					{ID: "pane-main", Title: "shell", Kind: WorkbenchPaneTerminalLive},
					{ID: "pane-two", Title: "logs", Kind: WorkbenchPaneTerminalLive, TerminalID: "terminal-1"},
				},
				RootSplit: WorkbenchSplitNode{
					Direction: WorkbenchSplitVertical,
					Children:  []WorkbenchSplitNode{{PaneID: "pane-main"}, {PaneID: "pane-two"}},
					Ratio:     0.5,
				},
			}},
		}},
	}
	resultPayload, err := EncodeMethodResult("workbench.apply", WorkbenchMutateResult{
		Snapshot:   snapshot,
		Action:     WorkbenchMutationPaneSplit,
		ResourceID: "pane-two",
	})
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}
	var out WorkbenchMutateResult
	if err := DecodeMethodResult("workbench.apply", resultPayload, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if out.Snapshot.Workspaces[0].Tabs[0].RootSplit.Direction != WorkbenchSplitVertical || out.ResourceID != "pane-two" {
		t.Fatalf("result roundtrip mismatch: %+v", out)
	}
}

func TestWorkbenchEventRoundTrip(t *testing.T) {
	event := Event{
		Type: EventWorkbenchChanged,
		Workbench: &WorkbenchChangedData{
			WorkspaceID: "workspace-main",
			Version:     9,
			Action:      string(WorkbenchMutationTabCreate),
			ResourceID:  "tab-two",
		},
	}
	payload, err := EncodeEventPayload(event)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	out, err := DecodeEventPayload(payload)
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if out.Type != EventWorkbenchChanged || out.Workbench == nil || out.Workbench.ResourceID != "tab-two" {
		t.Fatalf("event roundtrip mismatch: %+v", out)
	}
}
