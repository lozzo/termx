package canvas

import "strings"

func PadRight(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(value) >= width {
		return value[:width]
	}
	return value + strings.Repeat(" ", width-len(value))
}
