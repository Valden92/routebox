package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Valden92/routebox/internal/api"
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
)

func main() {
	var (
		dataDir = flag.String("data", defaultDataDir(), "config data directory")
		listen  = flag.String("listen", "127.0.0.1:47891", "API listen address")
		unlock  = flag.String("unlock", "", "passphrase if config is encrypted")
	)
	flag.Parse()

	store, err := initializeStore(*dataDir, *listen, *unlock)
	if err != nil {
		log.Fatal(err)
	}

	st := store.Get()
	bin := singbox.ResolveBin(st.SingBoxPath)
	if !store.IsLocked() && !singbox.HostReady(bin) {
		log.Printf("WARN: личный VPN недоступен — выполните: make sync (setcap + NetworkManager)")
	}
	sb := singbox.NewManager(bin, st.SingBoxConfigPath)
	srv := api.NewServer(store, sb)

	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("vpn-router API listening on %s", *listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.StopRouter()
	_ = httpServer.Shutdown(ctx)
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
