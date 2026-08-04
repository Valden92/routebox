package network

import (
	"net"
	"net/http"
	"time"
)

func HTTPClient(bindIface string, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	if bindIface != "" {
		dialer.Control = bindControl(bindIface)
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}
