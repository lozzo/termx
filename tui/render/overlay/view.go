package overlay

import featureoverlay "github.com/lozzow/termx/tui/features/overlay"

func Render(kind featureoverlay.Kind) string {
	switch kind {
	case featureoverlay.KindConnectPicker:
		return "overlay: connect picker"
	default:
		return ""
	}
}
