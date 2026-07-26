//go:build android && cgo

package main

/*
#cgo LDFLAGS: -llog
#include <android/log.h>
#include <stdlib.h>

static void muxvia_android_log(const char *message) {
  __android_log_write(ANDROID_LOG_WARN, "MuxviaGoClient", message);
}
*/
import "C"

import (
	"log"
	"strings"
	"unsafe"
)

type androidLogWriter struct{}

func (androidLogWriter) Write(payload []byte) (int, error) {
	message := C.CString(strings.TrimSpace(string(payload)))
	defer C.free(unsafe.Pointer(message))
	C.muxvia_android_log(message)
	return len(payload), nil
}

// configureAndroidLogging 把 Go Client Engine 的脱敏诊断接入 logcat。
// 当前标准 logger 只记录 pairing 和建链失败链，不记录 ticket、grant、credential 或 bridge token。
func configureAndroidLogging() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(androidLogWriter{})
}
