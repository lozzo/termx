module github.com/lozzow/termx/private/cloud/companion

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx v0.0.0
	github.com/zalando/go-keyring v0.2.8
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
)

replace github.com/lozzow/termx => ../../..
