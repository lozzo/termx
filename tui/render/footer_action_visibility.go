package render

import "strings"

func footerActionTokensVisibleByModeAndWidth(actions []FooterActionVM, mode string, width int) []FooterActionVM {
	if mode == "resize" {
		return footerResizeActionTokensVisibleForWidth(actions, width)
	}
	if width >= 100 || (mode != "live" && mode != "normal") || !footerActionsMatchKeys(actions, []string{"^P", "^R", "^T", "^W", "^O", "^V", "^F", "^G"}) {
		return actions
	}
	keys := []string{"^P", "^R", "^T", "^G"}
	if width < 72 {
		keys = []string{"^P", "^R", "^G"}
	}
	if width < 56 {
		keys = []string{"^P", "^G"}
	}
	return footerActionsByKeys(actions, keys)
}

func footerResizeActionTokensVisibleForWidth(actions []FooterActionVM, width int) []FooterActionVM {
	if width >= 100 {
		return actions
	}
	// 中文说明：resize 窄宽 footer 不能只保留 global；center/reset 是当前 mode 仍可执行的 view-layout 入口。
	ids := []ProjectionID{
		ActionResizeLayoutCenter,
		ActionResizeLeft,
		ActionResizeRight,
		ActionResizeLayoutReset,
		ActionFooterGlobalMode,
	}
	if width < 72 {
		ids = []ProjectionID{
			ActionResizeLayoutCenter,
			ActionResizeLayoutReset,
			ActionFooterGlobalMode,
		}
	}
	out := footerActionsByActionIDs(actions, ids)
	if len(out) == 0 {
		return actions
	}
	return out
}

func footerActionsMatchKeys(actions []FooterActionVM, keys []string) bool {
	if len(actions) != len(keys) {
		return false
	}
	for index, key := range keys {
		if strings.TrimSpace(actions[index].Key) != key {
			return false
		}
	}
	return true
}

func footerActionsByKeys(actions []FooterActionVM, keys []string) []FooterActionVM {
	out := make([]FooterActionVM, 0, len(keys))
	for _, key := range keys {
		for _, action := range actions {
			if strings.TrimSpace(action.Key) == key {
				out = append(out, action)
				break
			}
		}
	}
	return out
}

func footerActionsByActionIDs(actions []FooterActionVM, ids []ProjectionID) []FooterActionVM {
	out := make([]FooterActionVM, 0, len(ids))
	for _, id := range ids {
		actionID := id.String()
		for _, action := range actions {
			if strings.TrimSpace(action.ActionID) == actionID {
				out = append(out, action)
				break
			}
		}
	}
	return out
}
