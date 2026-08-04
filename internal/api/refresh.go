package api

import (
	"context"
	"log"
	"time"

	"github.com/dzaytsev/vpn-router/internal/config"
	"github.com/dzaytsev/vpn-router/internal/subscription"
)

type refreshScheduler struct {
	store *config.Store
	stop  chan struct{}
}

func newRefreshScheduler(store *config.Store) *refreshScheduler {
	return &refreshScheduler{store: store, stop: make(chan struct{})}
}

func (r *refreshScheduler) Start() {
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-tick.C:
				r.tick()
			}
		}
	}()
}

func (r *refreshScheduler) tick() {
	st := r.store.Get()
	now := time.Now()
	for _, sub := range st.Subscriptions {
		interval := sub.RefreshInterval()
		if !sub.AutoRefresh || interval <= 0 {
			continue
		}
		if !sub.LastRefresh.IsZero() && now.Sub(sub.LastRefresh) < interval {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		body, err := subscription.Fetch(ctx, sub.URL)
		cancel()
		if err != nil {
			log.Printf("subscription %s refresh: %v", sub.ID, err)
			continue
		}
		nodes, err := subscription.ParseBody(body)
		if err != nil {
			continue
		}
		_ = subscription.SaveCache(st.DataDir, sub.ID, nodes)
		_ = r.store.Update(func(cur *config.Settings) {
			for i := range cur.Subscriptions {
				if cur.Subscriptions[i].ID == sub.ID {
					cur.Subscriptions[i].LastRefresh = now
				}
			}
		})
	}
}
