module github.com/lozzow/termx/termx-core-v2

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx/internal v0.0.0
	github.com/lozzow/termx/termx-proto v0.0.0
	github.com/lozzow/termx/termx-shared v0.0.0
)

require (
	github.com/klauspost/compress v1.18.5 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/lozzow/termx/termx-shared => ../termx-shared

replace github.com/lozzow/termx/internal => ../internal

replace github.com/lozzow/termx/termx-proto => ../termx-proto
