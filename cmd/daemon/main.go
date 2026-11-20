package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Valden92/routebox/internal/api"
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/daemonlock"
	"github.com/Valden92/routebox/internal/singbox"
)

func main() {
	if err := runDaemon(); err != nil {
		log.Print(err)
		if errors.Is(err, daemonlock.ErrHeld) {
			os.Exit(73)
		}
		os.Exit(1)
	}
}

func runDaemon() error {
	var (
		dataDir = flag.String("data", defaultDataDir(), "config data directory")
		listen  = flag.String("listen", "127.0.0.1:47891", "API listen address")
		unlock  = flag.String("unlock", "", "passphrase if config is encrypted")
	)
	flag.Parse()

	if err := api.ValidateListenAddress(*listen); err != nil {
		return err
	}
	// One owner per user, independent of --data/--listen. The lock precedes
	// every credential/settings write, network probe and routing worker.
	owner, err := daemonlock.Acquire(daemonlock.Path())
	if err != nil {
		return err
	}
	defer owner.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	// Bind before publishing a new credential or starting routing workers.
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	access, err := api.NewLocalAccess(defaultDataDir())
	if err != nil {
		return err
	}

	store, err := initializeStore(*dataDir, *listen, *unlock)
	if err != nil {
		return err
	}

	st := store.Get()
	bin := singbox.ResolveBin(st.SingBoxPath)
	if !store.IsLocked() && !singbox.HostReady(bin) {
		log.Printf("WARN: личный VPN недоступен — выполните: make sync (setcap + NetworkManager)")
	}
	sb := singbox.NewManager(bin, st.SingBoxConfigPath)
	if ctx.Err() != nil {
		return nil
	}
	srv := api.NewServer(store, sb)

	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           access.Handler(srv.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("vpn-router API listening on %s", *listen)
	return serveUntilStopped(ctx, httpServer, listener, srv.StopRouter)
}

// A failed listener uses the same graceful cleanup as an explicit stop.
func serveUntilStopped(ctx context.Context, server *http.Server, listener net.Listener, stop func()) error {
	defer stop()
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-result:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}

// initializeStore never writes defaults over settings awaiting a password.
func initializeStore(dataDir, listen, passphrase string) (*config.Store, error) {
	store, err := config.NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	if err := store.TryLoad(); err != nil {
		switch {
		case errors.Is(err, config.ErrLocked):
			if passphrase == "" {
				// Serve only health/version/unlock until a password is supplied.
				return store, nil
			}
			if unlockErr := store.Unlock(passphrase); unlockErr != nil {
				return nil, unlockErr
			}
		case os.IsNotExist(err):
			// Update below creates the initial plaintext file atomically.
		default:
			return nil, err
		}
	}
	if err := store.Update(func(st *config.Settings) { st.APIListen = listen }); err != nil {
		return nil, err
	}
	return store, nil
}

func defaultDataDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, config.AppName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", config.AppName)
}
