module github.com/lozzow/termx/termx-shared

go 1.26.0

toolchain go1.26.1

require (
	github.com/klauspost/compress v1.18.5
	github.com/lozzow/termx/termx-proto v0.0.0
	google.golang.org/protobuf v1.36.11
)

replace github.com/lozzow/termx/termx-proto => ../termx-proto
