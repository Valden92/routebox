//go:build linux

package network

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func bindControl(iface string) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var opErr error
		err := c.Control(func(fd uintptr) {
			opErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface)
		})
		if err != nil {
			return err
		}
		return opErr
	}
}
