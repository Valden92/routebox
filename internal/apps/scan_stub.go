//go:build !linux

package apps

import "context"

type NetworkApp struct {
	PID         int    `json:"pid"`
	ProcessName string `json:"processName"`
	ExecPath    string `json:"execPath,omitempty"`
	Connections int    `json:"connections"`
}

func Scan(ctx context.Context) ([]NetworkApp, error) {
	return nil, nil
}
