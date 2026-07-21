//go:build !muxvia_android_spike

package main

import "fmt"

func newAndroidSpikeHost(string) (androidHost, error) {
	return nil, fmt.Errorf("Android spike harness is not included in this build")
}
