package app

import (
	"unicode/utf8"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/shared"
)

func presentedRowHasWidthSafetyState(row presentedRow) bool {
	return row.hasWide ||
		row.hasErase ||
		row.hasHiddenEmojiCompensation ||
		row.hasHostWidthStabilizer
}

func lineHasWidthSafetyState(line string) bool {
	if line == "" {
		return false
	}
	if fast, ok := asciiLineHasWidthSafetyState(line); ok {
		return fast
	}
	return genericLineHasWidthSafetyState(line)
}

func asciiLineHasWidthSafetyState(line string) (bool, bool) {
	for i := 0; i < len(line); {
		b := line[i]
		if b == '\x1b' {
			if i+1 >= len(line) || line[i+1] != '[' {
				return false, false
			}
			j := i + 2
			for j < len(line) && (line[j] < '@' || line[j] > '~') {
				if line[j] >= utf8.RuneSelf {
					return false, false
				}
				j++
			}
			if j >= len(line) {
				return false, false
			}
			if line[j] == 'X' {
				return true, true
			}
			i = j + 1
			continue
		}
		if b >= utf8.RuneSelf || b < 0x20 || b == 0x7f {
			return false, false
		}
		i++
	}
	return false, true
}

func genericLineHasWidthSafetyState(line string) bool {
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)
	state := byte(0)
	rest := line
	widthSafety := shared.WidthSafetyTracker{}
	for len(rest) > 0 {
		seq, width, n, nextState := xansi.DecodeSequence(rest, state, parser)
		if n <= 0 {
			break
		}
		token := string(seq)
		if width > 0 {
			transition := widthSafety.ObserveDisplayedCluster(token, width)
			if width != 1 || transition.ReanchorBefore {
				return true
			}
		} else if len(token) > 0 && token[0] != '\x1b' {
			widthSafety.ObserveNonPrintingCluster(token, width)
		} else if len(token) > 0 && token[0] == '\x1b' {
			switch xansi.Cmd(parser.Command()).Final() {
			case 'G':
				transition := widthSafety.ObserveReanchorBeforeNextCluster()
				if transition.HostWidthStabilizer {
					return true
				}
			case 'X':
				return true
			}
		}
		state = nextState
		rest = rest[n:]
	}
	return false
}

func presentedCellSafeForLinearDiff(cell presentedCell) bool {
	return cell.Width == 1 && !cell.ReanchorBefore && !cell.Erase
}

func presentedRowSafeForLinearOps(row presentedRow) bool {
	if presentedRowHasWidthSafetyState(row) {
		return false
	}
	for _, cell := range row.cells {
		if !presentedCellSafeForLinearDiff(cell) {
			return false
		}
	}
	return true
}

func hostCellHasWidthSafetyState(cell hostCell) bool {
	return cell.Wide ||
		cell.Continuation ||
		cell.HiddenEmojiCompensation ||
		cell.HostWidthStabilizer
}
