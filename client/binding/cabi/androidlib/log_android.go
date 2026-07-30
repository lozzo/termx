//go:build android && cgo

package main

import (
	"io"
	"log"
)

// Android c-shared 永久静默；不得把 binding、Pion 或错误链写入 logcat/stderr。
func configureAndroidLogging() {
	log.SetFlags(0)
	log.SetOutput(io.Discard)
}
