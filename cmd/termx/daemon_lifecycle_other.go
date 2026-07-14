//go:build !darwin && !linux

package main

import (
	"fmt"
	"os"
)

func daemonLifecycleSupported() bool { return false }
func daemonProcessIdentity(int) (string, error) {
	return "", fmt.Errorf("daemon lifecycle is unsupported")
}
func daemonRecordOwnedByCurrentUser(os.FileInfo) bool { return false }
func stopDaemonProcess(int) error                     { return fmt.Errorf("daemon lifecycle is unsupported") }
func startDetachedDaemon(string, string, string, bool) error {
	return fmt.Errorf("daemon lifecycle is unsupported")
}
