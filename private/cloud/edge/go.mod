module github.com/muxvia/muxvia/private/cloud/edge

go 1.26.0

require (
	github.com/muxvia/muxvia v0.0.0
	github.com/muxvia/muxvia/private/cloud/companion v0.0.0
	github.com/muxvia/muxvia/private/cloud/control-plane v0.0.0
	github.com/muxvia/muxvia/private/cloud/hub v0.0.0
	github.com/muxvia/muxvia/private/cloud/relay v0.0.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/pion/dtls/v3 v3.1.2 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/stun/v3 v3.1.1 // indirect
	github.com/pion/transport/v4 v4.0.1 // indirect
	github.com/pion/turn/v4 v4.1.4 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	github.com/zalando/go-keyring v0.2.8 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/muxvia/muxvia => ../../..

replace github.com/muxvia/muxvia/private/cloud/companion => ../companion

replace github.com/muxvia/muxvia/private/cloud/control-plane => ../control-plane

replace github.com/muxvia/muxvia/private/cloud/hub => ../hub

replace github.com/muxvia/muxvia/private/cloud/relay => ../relay
