module github.com/lozzow/termx/private/termx-cloud/companion

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx/termx-proto v0.0.0
	github.com/lozzow/termx/termx-shared v0.0.0
	google.golang.org/protobuf v1.36.11
)

replace github.com/lozzow/termx/termx-proto => ../../../termx-proto

replace github.com/lozzow/termx/termx-shared => ../../../termx-shared
