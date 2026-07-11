//go:build linux

package ipc

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func verifyLocalClient(conn net.Conn) error { return verifyUnixPeer(conn) }

func verifyLocalServer(conn net.Conn) error { return verifyUnixPeer(conn) }

func verifyUnixPeer(conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("Cloud Companion peer is not a Unix connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("inspect Cloud Companion peer: %w", err)
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("inspect Cloud Companion peer fd: %w", err)
	}
	if controlErr != nil {
		return fmt.Errorf("read Cloud Companion peer credential: %w", controlErr)
	}
	if credential == nil || credential.Uid != uint32(os.Getuid()) {
		return syscall.EACCES
	}
	return nil
}
