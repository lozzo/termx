module github.com/lozzow/termx/termx-testkit

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx/internal v0.0.0
	github.com/lozzow/termx/termx-core v0.0.0
	github.com/lozzow/termx/termx-proto v0.0.0
	github.com/lozzow/termx/termx-shared v0.0.0
)

require (
	github.com/charmbracelet/colorprofile v0.4.2 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260303162955-0b88c25f3fff // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/vt v0.0.0-20260316093931-f2fb44ab3145 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/lozzow/termx/termx-vterm v0.0.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/charmbracelet/x/vt => ../termx-vterm/third_party/github.com/charmbracelet/x/vt

replace github.com/lozzow/termx/internal => ../internal

replace github.com/lozzow/termx/termx-core => ../termx-core

replace github.com/lozzow/termx/termx-proto => ../termx-proto

replace github.com/lozzow/termx/termx-shared => ../termx-shared

replace github.com/lozzow/termx/termx-vterm => ../termx-vterm
