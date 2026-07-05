package app

import (
	"strings"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func applyConfiguredShellChrome(shell state.ShellStore, cfg state.TUIConfigStore) state.ShellStore {
	switch presentation := state.PanelPresentation(strings.TrimSpace(cfg.Chrome.PanelPresentation)); presentation {
	case state.PanelPresentationCard, state.PanelPresentationSplitLine:
		// 中文说明：panel presentation 是本地 chrome 偏好；配置在 runtime 启动/
		// workbench restore 边界写入 ShellStore，之后按键切换仍由 ShellStore 持有。
		return shell.SetPanelPresentation(presentation)
	default:
		return shell
	}
}
