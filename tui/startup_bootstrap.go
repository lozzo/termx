package tui

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app/intent"
	"github.com/lozzow/termx/tui/app/reducer"
	"github.com/lozzow/termx/tui/domain/types"
)

type StartupTaskExecutor interface {
	Execute(ctx context.Context, client Client, size protocol.Size, plan StartupPlan) (StartupPlan, error)
}

type startupTaskExecutor struct{}

func NewStartupTaskExecutor() StartupTaskExecutor {
	return startupTaskExecutor{}
}

// Execute 负责把 planner 产出的启动任务落实到 runtime 依赖上，再把结果回填到纯状态。
// 这样后续真正接回 Run 时，启动阶段仍然保持“先规划、后执行、再回填”的稳定边界。
func (startupTaskExecutor) Execute(ctx context.Context, client Client, size protocol.Size, plan StartupPlan) (StartupPlan, error) {
	state := plan.State
	for _, task := range plan.Tasks {
		switch taskValue := task.(type) {
		case CreateTerminalTask:
			next, err := executeCreateTerminalTask(ctx, client, size, state, taskValue)
			if err != nil {
				return StartupPlan{}, err
			}
			state = next
		case AttachTerminalTask:
			next, err := executeAttachTerminalTask(ctx, client, state, taskValue)
			if err != nil {
				return StartupPlan{}, err
			}
			state = next
		default:
			return StartupPlan{}, fmt.Errorf("unsupported startup task %T", task)
		}
	}
	return StartupPlan{
		State:    state,
		Warnings: append([]string(nil), plan.Warnings...),
	}, nil
}

func executeCreateTerminalTask(ctx context.Context, client Client, size protocol.Size, state types.AppState, task CreateTerminalTask) (types.AppState, error) {
	created, err := client.Create(ctx, append([]string(nil), task.Command...), task.Name, size)
	if err != nil {
		return types.AppState{}, err
	}
	terminalID := types.TerminalID(created.TerminalID)
	state.Domain.Terminals[terminalID] = types.TerminalRef{
		ID:      terminalID,
		Name:    task.Name,
		Command: append([]string(nil), task.Command...),
		State:   terminalRunStateFromProtocol(created.State),
	}
	result := reducer.New().Reduce(state, intent.ConnectTerminalIntent{
		PaneID:     task.PaneID,
		TerminalID: terminalID,
		Source:     intent.ConnectSourceRestore,
	})
	return result.State, nil
}

func executeAttachTerminalTask(ctx context.Context, client Client, state types.AppState, task AttachTerminalTask) (types.AppState, error) {
	list, err := client.List(ctx)
	if err != nil {
		return types.AppState{}, err
	}
	info, ok := findTerminalInfo(list, task.TerminalID)
	if !ok {
		return types.AppState{}, fmt.Errorf("attach terminal %q not found", task.TerminalID)
	}
	terminalID := types.TerminalID(info.ID)
	state.Domain.Terminals[terminalID] = types.TerminalRef{
		ID:       terminalID,
		Name:     info.Name,
		Command:  append([]string(nil), info.Command...),
		Tags:     cloneTags(info.Tags),
		State:    terminalRunStateFromProtocol(info.State),
		ExitCode: info.ExitCode,
	}
	result := reducer.New().Reduce(state, intent.ConnectTerminalIntent{
		PaneID:     task.PaneID,
		TerminalID: terminalID,
		Source:     intent.ConnectSourceRestore,
	})
	return result.State, nil
}

func terminalRunStateFromProtocol(state string) types.TerminalRunState {
	switch state {
	case "exited":
		return types.TerminalRunStateExited
	case "stopped":
		return types.TerminalRunStateStopped
	default:
		return types.TerminalRunStateRunning
	}
}

func findTerminalInfo(list *protocol.ListResult, terminalID types.TerminalID) (protocol.TerminalInfo, bool) {
	if list == nil {
		return protocol.TerminalInfo{}, false
	}
	for _, item := range list.Terminals {
		if item.ID == string(terminalID) {
			return item, true
		}
	}
	return protocol.TerminalInfo{}, false
}

func cloneTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}
