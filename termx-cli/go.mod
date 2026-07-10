module github.com/lozzow/termx/termx-cli

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx/termx-core-v2 v0.0.0
	github.com/lozzow/termx/termx-hub v0.0.0
	github.com/lozzow/termx/termx-remote-v2 v0.0.0
	github.com/lozzow/termx/termx-tui-v3 v0.0.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/term v0.41.0
)

require (
	github.com/lozzow/termx/termx-vterm v0.0.0 // indirect
	github.com/pion/datachannel v1.6.0 // indirect
	github.com/pion/dtls/v3 v3.1.2 // indirect
	github.com/pion/ice/v4 v4.2.1 // indirect
	github.com/pion/interceptor v0.1.44 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.1.0 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.16 // indirect
	github.com/pion/rtp v1.10.1 // indirect
	github.com/pion/sctp v1.9.2 // indirect
	github.com/pion/sdp/v3 v3.0.18 // indirect
	github.com/pion/srtp/v3 v3.0.10 // indirect
	github.com/pion/stun/v3 v3.1.1 // indirect
	github.com/pion/transport/v4 v4.0.1 // indirect
	github.com/pion/turn/v4 v4.1.4 // indirect
	github.com/pion/webrtc/v4 v4.2.9 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/time v0.10.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/grpc v1.64.0 // indirect
)

require (
	charm.land/lipgloss/v2 v2.0.2 // indirect
	github.com/charmbracelet/colorprofile v0.4.2 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260303162955-0b88c25f3fff // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/google/uuid v1.6.0
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/lozzow/termx/internal v0.0.0
	github.com/lozzow/termx/termx-proto v0.0.0
	github.com/lozzow/termx/termx-shared v0.0.0
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/lozzow/termx/termx-core-v2 => ../termx-core-v2

replace github.com/lozzow/termx/termx-hub => ../termx-hub

replace github.com/lozzow/termx/termx-remote-v2 => ../termx-remote-v2

replace github.com/lozzow/termx/termx-tui-v3 => ../termx-tui-v3

replace github.com/lozzow/termx/termx-vterm => ../termx-vterm

replace github.com/lozzow/termx/termx-shared => ../termx-shared

replace github.com/lozzow/termx/termx-proto => ../termx-proto

replace github.com/lozzow/termx/internal => ../internal

replace github.com/lozzow/termx/termx-testkit => ../termx-testkit
