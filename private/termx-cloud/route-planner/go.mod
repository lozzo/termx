module github.com/lozzow/termx/private/termx-cloud/route-planner

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx/termx-proto v0.0.0
	github.com/lozzow/termx/termx-shared v0.0.0
)

replace github.com/lozzow/termx/termx-proto => ../../../termx-proto

replace github.com/lozzow/termx/termx-shared => ../../../termx-shared
