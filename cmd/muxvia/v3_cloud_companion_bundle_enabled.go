//go:build muxvia_cloud_bundled

package main

import _ "embed"

// cloudDevelopmentCompanionEmbedded 是构建脚本生成并由 Go linker 固化进 muxvia 的 Companion artifact。
//
//go:embed cloud_bundle/muxvia-cloud
var cloudDevelopmentCompanionEmbedded []byte
