package app

import (
	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
)

func actionIDForShellAction(action input.ShellAction, reason string) (render.ActionID, bool) {
	switch action {
	case input.ShellActionToggleHeader:
		return render.ActionFooterToggleHeader, true
	case input.ShellActionToggleFooter:
		return render.ActionFooterToggleFooter, true
	case input.ShellActionClearToasts:
		return render.ActionFooterClearToasts, true
	case input.ShellActionCloseToast:
		return render.ActionFooterCloseToast, true
	case input.ShellActionOpenPool:
		return render.ActionFooterOpenPool, true
	case input.ShellActionOpenTree:
		return render.ActionFooterOpenTree, true
	case input.ShellActionOpenPicker:
		return render.ActionFooterPicker, true
	case input.ShellActionOpenPrompt:
		return render.ActionPromptOpen, true
	case input.ShellActionOpenHelp:
		return render.ActionHelpOpen, true
	case input.ShellActionOpenClipboardHistory:
		return render.ActionClipboardHistoryOpen, true
	case input.ShellActionQuit:
		return render.ActionFooterQuit, true
	case input.ShellActionFloatingNew:
		return render.ActionFloatingNew, true
	case input.ShellActionFloatingOverview:
		return render.ActionFloatingOverview, true
	case input.ShellActionFloatingSummon:
		return render.ActionFloatingSummon, true
	case input.ShellActionFloatingCtrl:
		switch reason {
		case "close":
			return render.ActionFloatingClose, true
		case "collapse":
			return render.ActionFloatingCollapse, true
		case "center":
			return render.ActionFloatingCenter, true
		default:
			return "", false
		}
	case input.ShellActionFloatingGroup:
		switch reason {
		case "toggle-all":
			return render.ActionFloatingToggleAll, true
		case "fit":
			return render.ActionFloatingFit, true
		case "toggle-auto-fit":
			return render.ActionFloatingAutoFit, true
		default:
			return "", false
		}
	case input.ShellActionFloatingMove:
		switch reason {
		case "left":
			return render.ActionFloatingMoveLeft, true
		case "right":
			return render.ActionFloatingMoveRight, true
		case "up":
			return render.ActionFloatingMoveUp, true
		case "down":
			return render.ActionFloatingMoveDown, true
		default:
			return "", false
		}
	case input.ShellActionFloatingSize:
		switch reason {
		case "narrow":
			return render.ActionFloatingNarrow, true
		case "wide":
			return render.ActionFloatingWide, true
		case "short":
			return render.ActionFloatingShort, true
		case "tall":
			return render.ActionFloatingTall, true
		default:
			return "", false
		}
	default:
		return "", false
	}
}
