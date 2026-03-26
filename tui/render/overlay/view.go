package overlay

import featureoverlay "github.com/lozzow/termx/tui/features/overlay"

func Render(kind featureoverlay.Kind) string {
	switch kind {
	case featureoverlay.KindConnectPicker:
		return "overlay: connect picker"
	case featureoverlay.KindHelp:
		return "overlay: help"
	case featureoverlay.KindPrompt:
		return "overlay: prompt"
	default:
		return ""
	}
}
