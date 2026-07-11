module github.com/lozzow/termx/termx-shared

go 1.26.0

toolchain go1.26.1

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/klauspost/compress v1.18.5
	github.com/lozzow/termx/termx-proto v0.0.0
	golang.org/x/mod v0.32.0
	golang.org/x/sys v0.46.0
	google.golang.org/protobuf v1.36.11
)

replace github.com/lozzow/termx/termx-proto => ../termx-proto
