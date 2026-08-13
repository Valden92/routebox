package api

import (
	"context"
	"log"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/subscription"
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
		if !sub.AutoRefresh || interval <= 0 || !subscription.IsRemoteURL(sub.URL) {
			continue
		}
		if !sub.LastRefresh.IsZero() && now.Sub(sub.LastRefresh) < interval {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		fetched, err := subscription.Fetch(ctx, sub.URL)
		cancel()
		if err != nil {
			log.Printf("subscription %s refresh: %v", sub.ID, err)
			continue
		}
		nodes, err := subscription.ParseBody(fetched.Body)
		if err != nil {
			continue
		}
		nodes = subscription.FilterValidNodes(nodes)
		if len(nodes) == 0 {
			continue
		}
		previous, _ := subscription.LoadCache(st.DataDir, sub.ID)
		_ = subscription.SaveCache(st.DataDir, sub.ID, nodes)
		reconcileSelectedNode(r.store, sub.ID, previous.Nodes, nodes)
		_ = r.store.Update(func(cur *config.Settings) {
			for i := range cur.Subscriptions {
				if cur.Subscriptions[i].ID == sub.ID {
					cur.Subscriptions[i].LastRefresh = now
					applySubscriptionMeta(&cur.Subscriptions[i], fetched.Meta)
				}
			}
		})
	}
}

// reconcileSelectedNode — копия логики Server.reconcileSelectedNodeAfterRefresh для scheduler.
func reconcileSelectedNode(store *config.Store, subID string, previous, next []subscription.Node) {
	_ = store.Update(func(cur *config.Settings) {
		for i := range cur.Subscriptions {
			if cur.Subscriptions[i].ID != subID {
				continue
			}
			id, counts := subscription.RemapSelection(
				cur.Subscriptions[i].SelectedNodeID,
				cur.Subscriptions[i].NodeSelectCounts,
				previous,
				next,
			)
			cur.Subscriptions[i].SelectedNodeID = id
			cur.Subscriptions[i].NodeSelectCounts = counts
			return
		}
	})
}
