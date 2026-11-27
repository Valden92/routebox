//go:build !linux

package apps

import (
	"context"
	"errors"
)

func Scan(ctx context.Context) ([]NetworkApp, error) {
	return nil, errors.New("снимок сетевых сокетов доступен только в Linux")
}
