package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
	"go.yaml.in/yaml/v3"
)

type StartupPlanner interface {
	Plan(ctx context.Context, cfg Config) (StartupPlan, error)
}

type LayoutLoader interface {
	LoadLayout(ctx context.Context, ref string) ([]byte, error)
}

type StartupTask interface {
	startupTaskName() string
}

type CreateTerminalTask struct {
	PaneID  types.PaneID
	Command []string
	Name    string
}

func (CreateTerminalTask) startupTaskName() string { return "create_terminal" }

type StartupPlan struct {
	State    types.AppState
	Tasks    []StartupTask
	Warnings []string
}

type startupPlanner struct {
	layouts LayoutLoader
}

type fileLayoutLoader struct{}

type startupLayoutDocument struct {
	Workspace string            `yaml:"workspace"`
	Tab       string            `yaml:"tab"`
	Slot      startupLayoutSlot `yaml:"slot"`
}

type startupLayoutSlot struct {
	Role string `yaml:"role"`
	Hint string `yaml:"hint"`
}

func NewStartupPlanner(loader LayoutLoader) StartupPlanner {
	if loader == nil {
		loader = fileLayoutLoader{}
	}
	return startupPlanner{layouts: loader}
}

// Plan 先把启动阶段收敛成纯规划，避免 runtime 刚接回时就把加载、降级和 UI 初始化搅在一起。
// 这一层只负责生成初始状态和启动任务，不直接触碰 daemon、PTY 或 Bubble Tea 程序生命周期。
func (p startupPlanner) Plan(ctx context.Context, cfg Config) (StartupPlan, error) {
	if strings.TrimSpace(cfg.StartupLayout) == "" {
		return defaultStartupPlan(cfg), nil
	}
	plan, err := p.planFromLayout(ctx, cfg)
	if err == nil {
		return plan, nil
	}
	if !cfg.StartupAutoLayout {
		return StartupPlan{}, err
	}
	degraded := defaultStartupPlan(cfg)
	degraded.Warnings = append(degraded.Warnings, fmt.Sprintf("layout startup degraded: %v", err))
	return degraded, nil
}

func (p startupPlanner) planFromLayout(ctx context.Context, cfg Config) (StartupPlan, error) {
	content, err := p.layouts.LoadLayout(ctx, cfg.StartupLayout)
	if err != nil {
		return StartupPlan{}, err
	}
	doc, err := parseStartupLayout(content)
	if err != nil {
		return StartupPlan{}, err
	}
	state := buildSinglePaneAppState(firstNonEmpty(doc.Workspace, cfg.Workspace, "main"), firstNonEmpty(doc.Tab, "dev"), types.PaneSlotWaiting)
	result := reducer.New().Reduce(state, intent.OpenLayoutResolveIntent{
		PaneID: types.PaneID("pane-1"),
		Role:   doc.Slot.Role,
		Hint:   doc.Slot.Hint,
	})
	return StartupPlan{State: result.State}, nil
}

func defaultStartupPlan(cfg Config) StartupPlan {
	state := buildSinglePaneAppState(firstNonEmpty(cfg.Workspace, "main"), "shell", types.PaneSlotEmpty)
	return StartupPlan{
		State: state,
		Tasks: []StartupTask{CreateTerminalTask{
			PaneID:  types.PaneID("pane-1"),
			Command: []string{defaultShell(cfg)},
			Name:    "ws-1-tab-1-pane-1",
		}},
	}
}

func parseStartupLayout(content []byte) (startupLayoutDocument, error) {
	var doc startupLayoutDocument
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return startupLayoutDocument{}, fmt.Errorf("parse startup layout: %w", err)
	}
	return doc, nil
}

func buildSinglePaneAppState(workspaceName string, tabName string, slotState types.PaneSlotState) types.AppState {
	workspaceID := types.WorkspaceID("ws-1")
	tabID := types.TabID("tab-1")
	paneID := types.PaneID("pane-1")
	return types.AppState{
		Domain: types.DomainState{
			ActiveWorkspaceID: workspaceID,
			WorkspaceOrder:    []types.WorkspaceID{workspaceID},
			Workspaces: map[types.WorkspaceID]types.WorkspaceState{
				workspaceID: {
					ID:          workspaceID,
					Name:        workspaceName,
					ActiveTabID: tabID,
					TabOrder:    []types.TabID{tabID},
					Tabs: map[types.TabID]types.TabState{
						tabID: {
							ID:           tabID,
							Name:         tabName,
							ActivePaneID: paneID,
							ActiveLayer:  types.FocusLayerTiled,
							Panes: map[types.PaneID]types.PaneState{
								paneID: {
									ID:        paneID,
									Kind:      types.PaneKindTiled,
									SlotState: slotState,
								},
							},
							RootSplit: &types.SplitNode{PaneID: paneID},
						},
					},
				},
			},
			Terminals:   map[types.TerminalID]types.TerminalRef{},
			Connections: map[types.TerminalID]types.ConnectionState{},
		},
		UI: types.UIState{
			Focus: types.FocusState{
				Layer:       types.FocusLayerTiled,
				WorkspaceID: workspaceID,
				TabID:       tabID,
				PaneID:      paneID,
			},
			Overlay: types.OverlayState{Kind: types.OverlayNone},
			Mode:    types.ModeState{Active: types.ModeNone},
		},
	}
}

func defaultShell(cfg Config) string {
	if strings.TrimSpace(cfg.DefaultShell) != "" {
		return cfg.DefaultShell
	}
	return "/bin/sh"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (fileLayoutLoader) LoadLayout(_ context.Context, ref string) ([]byte, error) {
	for _, candidate := range layoutCandidates(ref) {
		content, err := os.ReadFile(candidate)
		if err == nil {
			return content, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("layout %q not found", ref)
}

func layoutCandidates(ref string) []string {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	candidates := []string{ref}
	if strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		return candidates
	}
	candidates = append(candidates, ref+".yaml", ref+".yml")
	return candidates
}
