package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPShutdownUsesGracefulRouterCleanup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	result := make(chan error, 1)
	go func() { result <- serveUntilStopped(ctx, server, listener, func() { close(stopped) }) }()
	client := http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("router cleanup was not called")
	}
}

type failedListener struct{}

func (failedListener) Accept() (net.Conn, error) { return nil, errors.New("fixture listener failed") }
func (failedListener) Close() error              { return nil }
func (failedListener) Addr() net.Addr            { return &net.TCPAddr{} }
func TestListenerFailureAlsoCleansUpRouter(t *testing.T) {
	stopped := false
	err := serveUntilStopped(context.Background(), &http.Server{ReadHeaderTimeout: time.Second}, failedListener{}, func() { stopped = true })
	if err == nil || !stopped {
		t.Fatalf("listener failure skipped cleanup: %v", err)
	}
}
