module github.com/lozzow/termx/private/cloud/edge

go 1.26.0

require (
	github.com/lozzow/termx v0.0.0
	github.com/lozzow/termx/private/cloud/companion v0.0.0
	github.com/lozzow/termx/private/cloud/control-plane v0.0.0
	github.com/lozzow/termx/private/cloud/hub v0.0.0
	github.com/lozzow/termx/private/cloud/relay v0.0.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pion/dtls/v3 v3.1.2 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/stun/v3 v3.1.1 // indirect
	github.com/pion/transport/v4 v4.0.1 // indirect
	github.com/pion/turn/v4 v4.1.4 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	github.com/zalando/go-keyring v0.2.8 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.74.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.53.0 // indirect
)

replace github.com/lozzow/termx => ../../..

replace github.com/lozzow/termx/private/cloud/companion => ../companion

replace github.com/lozzow/termx/private/cloud/control-plane => ../control-plane

replace github.com/lozzow/termx/private/cloud/hub => ../hub

replace github.com/lozzow/termx/private/cloud/relay => ../relay
