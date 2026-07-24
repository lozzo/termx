//go:build !muxvia_cloud_bundled

package main

// cloudDevelopmentCompanionEmbedded 仅由显式单文件 Cloud 构建替换；普通源码构建不携带私有 artifact。
var cloudDevelopmentCompanionEmbedded []byte
