//go:build !linux

package network

import "syscall"

func bindControl(iface string) func(network, address string, c syscall.RawConn) error {
	return nil
}
