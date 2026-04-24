package render

import (
	"strconv"
	"strings"
)

func offsetCHAANSI(s string, offset int) string {
	if offset == 0 || !strings.Contains(s, "\x1b[") {
		return s
	}
	var out strings.Builder
	changed := false
	for i := 0; i < len(s); {
		if s[i] != '\x1b' || i+2 >= len(s) || s[i+1] != '[' {
			out.WriteByte(s[i])
			i++
			continue
		}
		j := i + 2
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i+2 || j >= len(s) || s[j] != 'G' {
			out.WriteByte(s[i])
			i++
			continue
		}
		col, err := strconv.Atoi(s[i+2 : j])
		if err != nil {
			out.WriteString(s[i : j+1])
			i = j + 1
			continue
		}
		out.WriteString("\x1b[")
		out.WriteString(strconv.Itoa(maxInt(1, col+offset)))
		out.WriteByte('G')
		changed = true
		i = j + 1
	}
	if !changed {
		return s
	}
	return out.String()
}
