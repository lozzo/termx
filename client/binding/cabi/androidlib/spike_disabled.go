//go:build cgo && !anytty_android_spike

package main

import "fmt"

func androidSpikeUnavailable(string) error {
	return fmt.Errorf("Android spike harness is not included in this build")
}
