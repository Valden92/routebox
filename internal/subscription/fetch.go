package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	client := newFetchClient(net.DefaultResolver.LookupNetIP, dialer.DialContext)
	defer client.CloseIdleConnections()
	return fetchWithClient(ctx, subURL, client)
}

func fetchWithClient(ctx context.Context, subURL string, client *http.Client) (FetchResult, error) {
	u, err := parseFetchURL(subURL)
	if err != nil {
		return FetchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return FetchResult{}, errors.New("не удалось создать запрос подписки")
	}
	if u.User != nil {
		password, _ := u.User.Password()
		req.SetBasicAuth(u.User.Username(), password)
		req.URL.User = nil
	}
	// Многие провайдеры (quattro-cloud и др.) отдают HTML без клиентского UA
	req.Header.Set("User-Agent", "v2rayN/6.42.0")
	req.Header.Set("Accept", "*/*")
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, safeFetchError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FetchResult{}, fmt.Errorf("subscription HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes+1))
	if err != nil {
		return FetchResult{}, safeFetchError(err)
	}
	if len(body) > maxSubscriptionBytes {
		return FetchResult{}, fmt.Errorf("подписка превышает допустимый размер 8 МиБ")
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
