//go:build !linux

package daemonlock

import (
	"errors"
	"os"
)

var ErrHeld = errors.New("маршрутизатор уже обслуживается другим процессом")

// Path has no supported runtime directory on this platform.
func Path() string { return "" }

// Acquire fails closed: host-wide ownership is currently implemented on Linux.
func Acquire(_ string) (*os.File, error) {
	return nil, errors.New("владение маршрутизатором поддерживается только на Linux")
}
