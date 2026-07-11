module github.com/lozzow/termx/private/cloud/devcloud

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx v0.0.0
	github.com/lozzow/termx/private/cloud/companion v0.0.0
	github.com/lozzow/termx/private/cloud/control-plane v0.0.0
	github.com/lozzow/termx/private/cloud/hub v0.0.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/zalando/go-keyring v0.2.8 // indirect
	golang.org/x/sys v0.46.0 // indirect
)

replace github.com/lozzow/termx => ../../..

replace github.com/lozzow/termx/private/cloud/companion => ../companion

replace github.com/lozzow/termx/private/cloud/control-plane => ../control-plane

replace github.com/lozzow/termx/private/cloud/hub => ../hub
