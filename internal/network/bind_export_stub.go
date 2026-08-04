//go:build !linux

package network

import "syscall"

func BindControlExport(iface string) func(network, address string, c syscall.RawConn) error {
	return nil
}
