module github.com/lozzow/termx/private/cloud/hub

go 1.26.0

toolchain go1.26.1

require github.com/lozzow/termx/private/cloud/control-plane v0.0.0

replace github.com/lozzow/termx/private/cloud/control-plane => ../control-plane
