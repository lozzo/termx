package pool

import (
	"sort"
	"strings"

	"github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
)

type ConnectItem struct {
	TerminalID   types.TerminalID
	Name         string
	StateSummary string
	OwnerSummary string
}

// BuildConnectItems 只产出 connect dialog 需要的轻量列表视图。
// 排序优先级明确锁定为“最近用户交互”，不要被输出噪音覆盖。
func BuildConnectItems(terminals map[types.TerminalID]terminal.Metadata) []ConnectItem {
	items := make([]ConnectItem, 0, len(terminals))
	metas := make([]terminal.Metadata, 0, len(terminals))
	for _, meta := range terminals {
		metas = append(metas, meta)
	}

	sort.SliceStable(metas, func(i, j int) bool {
		if !metas[i].LastInteraction.Equal(metas[j].LastInteraction) {
			return metas[i].LastInteraction.After(metas[j].LastInteraction)
		}
		left := strings.ToLower(metas[i].Name)
		right := strings.ToLower(metas[j].Name)
		if left != right {
			return left < right
		}
		return metas[i].ID < metas[j].ID
	})

	for _, meta := range metas {
		items = append(items, ConnectItem{
			TerminalID:   meta.ID,
			Name:         meta.Name,
			StateSummary: stateSummary(meta),
			OwnerSummary: ownerSummary(meta),
		})
	}
	return items
}

func stateSummary(meta terminal.Metadata) string {
	switch meta.State {
	case terminal.StateExited:
		return "exited"
	default:
		return "running"
	}
}

func ownerSummary(meta terminal.Metadata) string {
	if meta.State == terminal.StateExited {
		return ""
	}
	if meta.OwnerPaneID == "" {
		return "no owner"
	}
	return "owner elsewhere"
}
