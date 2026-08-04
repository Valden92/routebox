package subscription

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func Fetch(ctx context.Context, subURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
	if err != nil {
		return nil, err
	}
	// Многие провайдеры (quattro-cloud и др.) отдают HTML без клиентского UA
	req.Header.Set("User-Agent", "v2rayN/6.42.0")
	req.Header.Set("Accept", "*/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscription HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func SaveCache(dataDir, subscriptionID string, nodes []Node) error {
	dir := filepath.Join(dataDir, "subscriptions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	c := Cache{
		SubscriptionID: subscriptionID,
		Nodes:          nodes,
		FetchedAt:      time.Now(),
	}
	path := filepath.Join(dir, subscriptionID+".json")
	return writeJSON(path, c)
}

func LoadCache(dataDir, subscriptionID string) (Cache, error) {
	path := filepath.Join(dataDir, "subscriptions", subscriptionID+".json")
	var c Cache
	err := readJSON(path, &c)
	return c, err
}
