package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dzaytsev/vpn-router/internal/api"
	"github.com/dzaytsev/vpn-router/internal/config"
	"github.com/dzaytsev/vpn-router/internal/singbox"
)

func main() {
	var (
		dataDir  = flag.String("data", defaultDataDir(), "config data directory")
		listen   = flag.String("listen", "127.0.0.1:47891", "API listen address")
		unlock   = flag.String("unlock", "", "passphrase if config is encrypted")
	)
	flag.Parse()

	store, err := config.NewStore(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if err := store.TryLoad(); err != nil {
		if os.IsNotExist(err) && *unlock != "" {
			if err := store.Unlock(*unlock); err != nil {
				log.Fatal(err)
			}
		} else if os.IsNotExist(err) {
			_ = store.SavePlain()
		} else if err != nil {
			log.Fatal(err)
		}
	}
	_ = store.Update(func(st *config.Settings) {
		st.APIListen = *listen
	})

	st := store.Get()
	bin := singbox.ResolveBin(st.SingBoxPath)
	if !singbox.HostReady(bin) {
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
	_ = sb.Stop()
	singbox.RestoreAfterPersonalVPNOn(st.MainInterface)
	_ = httpServer.Shutdown(ctx)
}

func defaultDataDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, config.AppName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", config.AppName)
}
