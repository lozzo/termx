module github.com/lozzow/termx/termx-remote

go 1.26.0

toolchain go1.26.1

require (
	github.com/pion/ice/v4 v4.2.1
	github.com/pion/stun/v3 v3.1.1
	github.com/pion/turn/v4 v4.1.4
	github.com/pion/webrtc/v4 v4.2.9
	github.com/soheilhy/cmux v0.1.5
	google.golang.org/grpc v1.64.0
	google.golang.org/protobuf v1.36.11
)

require github.com/lozzow/termx/termx-proto v0.0.0 // indirect

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/lozzow/termx/internal v0.0.0
	github.com/lozzow/termx/termx-shared v0.0.0
	github.com/pion/datachannel v1.6.0 // indirect
	github.com/pion/dtls/v3 v3.1.2 // indirect
	github.com/pion/interceptor v0.1.44 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.1.0 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.16 // indirect
	github.com/pion/rtp v1.10.1 // indirect
	github.com/pion/sctp v1.9.2 // indirect
	github.com/pion/sdp/v3 v3.0.18 // indirect
	github.com/pion/srtp/v3 v3.0.10 // indirect
	github.com/pion/transport/v4 v4.0.1 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	golang.org/x/time v0.10.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
)

replace github.com/lozzow/termx/termx-shared => ../termx-shared

replace github.com/lozzow/termx/termx-proto => ../termx-proto

replace github.com/lozzow/termx/internal => ../internal
