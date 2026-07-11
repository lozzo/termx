//go:build !windows && !darwin && !linux

package ipc

import (
	"fmt"
	"net"
)

func verifyLocalClient(net.Conn) error {
	return fmt.Errorf("Cloud Companion peer credential verification is unsupported on this platform")
}

func verifyLocalServer(net.Conn) error {
	return fmt.Errorf("Cloud Companion peer credential verification is unsupported on this platform")
}
