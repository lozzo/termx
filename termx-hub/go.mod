module github.com/lozzow/termx/termx-hub

go 1.26.0

require (
	github.com/lozzow/termx/termx-remote v0.0.0
	github.com/soheilhy/cmux v0.1.5
	google.golang.org/grpc v1.64.0
)

require (
	github.com/pion/dtls/v3 v3.1.2 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/stun/v3 v3.1.1 // indirect
	github.com/pion/transport/v4 v4.0.1 // indirect
	github.com/pion/turn/v4 v4.1.4 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/lozzow/termx/termx-core => ../termx-core

replace github.com/lozzow/termx/termx-remote => ../termx-remote

replace github.com/lozzow/termx/termx-vterm => ../termx-vterm

replace github.com/charmbracelet/x/vt => ../termx-vterm/third_party/github.com/charmbracelet/x/vt

replace github.com/lozzow/termx/termx-shared => ../termx-shared
