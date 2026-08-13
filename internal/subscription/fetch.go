package subscription

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FetchResult — тело подписки и метаданные из HTTP-заголовков.
type FetchResult struct {
	Body []byte
	Meta Meta
}

func Fetch(ctx context.Context, subURL string) (FetchResult, error) {
	if !IsRemoteURL(subURL) {
		return FetchResult{}, fmt.Errorf("нет HTTP(S) URL для обновления")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
	if err != nil {
		return FetchResult{}, err
	}
	// Многие провайдеры (quattro-cloud и др.) отдают HTML без клиентского UA
	req.Header.Set("User-Agent", "v2rayN/6.42.0")
	req.Header.Set("Accept", "*/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FetchResult{}, fmt.Errorf("subscription HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return FetchResult{}, err
	}
	return FetchResult{Body: body, Meta: MetaFromHeaders(resp.Header)}, nil
}

// IsRemoteURL — подписку можно обновлять по сети.
func IsRemoteURL(u string) bool {
	u = strings.TrimSpace(u)
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
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
