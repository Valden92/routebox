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
			sel := cur.Subscriptions[i].SelectedNodeID
			if sel == "" {
				return
			}
			if _, ok := subscription.FindNodeByID(next, sel); ok {
				return
			}
			if old, ok := subscription.FindNodeByID(previous, sel); ok {
				if remapped, ok := subscription.FindNodeByEndpoint(next, old.Host, old.Port, old.Protocol); ok {
					cur.Subscriptions[i].SelectedNodeID = remapped.ID
					if cur.Subscriptions[i].NodeSelectCounts == nil {
						cur.Subscriptions[i].NodeSelectCounts = make(map[string]int)
					}
					cur.Subscriptions[i].NodeSelectCounts[remapped.ID] += cur.Subscriptions[i].NodeSelectCounts[sel]
					delete(cur.Subscriptions[i].NodeSelectCounts, sel)
					return
				}
			}
			cur.Subscriptions[i].SelectedNodeID = ""
			if cur.Subscriptions[i].NodeSelectCounts != nil {
				delete(cur.Subscriptions[i].NodeSelectCounts, sel)
			}
			return
		}
	})
}
