module github.com/lozzow/termx/private/cloud/hub

go 1.26.0

toolchain go1.26.1

require (
	github.com/lozzow/termx v0.0.0
	github.com/lozzow/termx/private/cloud/control-plane v0.0.0
)

require (
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/lozzow/termx => ../../..

replace github.com/lozzow/termx/private/cloud/control-plane => ../control-plane
