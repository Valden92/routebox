package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/network"
)

type PathResult struct {
	Path       config.RoutePath `json:"path"`
	Available  bool             `json:"available"`
	StatusCode int              `json:"statusCode,omitempty"`
	LatencyMs  float64          `json:"latencyMs,omitempty"`
	Error      string           `json:"error,omitempty"`
	Skipped    bool             `json:"skipped,omitempty"`
	SkipReason string           `json:"skipReason,omitempty"`
}

type SiteReport struct {
	URL         string       `json:"url"`
	ResolvedIP  string       `json:"resolvedIp,omitempty"`
	Results     []PathResult `json:"results"`
	BestPath    string       `json:"bestPath,omitempty"`
	Unavailable bool         `json:"unavailable"`
	CheckedAt   time.Time    `json:"checkedAt"`
}

func CheckSite(ctx context.Context, rawURL string, paths []config.RoutePath, bindByPath map[config.RoutePath]string, skip map[config.RoutePath]string) SiteReport {
	report := SiteReport{
		URL:       rawURL,
		CheckedAt: time.Now(),
	}
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = "https://" + rawURL
	}
	report.URL = rawURL
	host := hostFromURL(rawURL)
	if ip, err := net.LookupHost(host); err == nil && len(ip) > 0 {
		report.ResolvedIP = ip[0]
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	results := make([]PathResult, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		if reason, ok := skip[p]; ok && reason != "" {
			results[i] = PathResult{
				Path: p, Skipped: true, SkipReason: reason,
			}
			continue
		}
		wg.Add(1)
		go func(i int, p config.RoutePath) {
			defer wg.Done()
			iface := bindByPath[p]
			r := PathResult{Path: p}
			start := time.Now()
			code, err := head(ctx, rawURL, iface)
			r.LatencyMs = float64(time.Since(start).Milliseconds())
			if err != nil {
				r.Error = err.Error()
			} else {
				r.StatusCode = code
				r.Available = isReachableStatus(code)
			}
			results[i] = r
		}(i, p)
	}
	wg.Wait()
	report.Results = results
	for _, r := range report.Results {
		if r.Available {
			report.BestPath = string(r.Path)
			report.Unavailable = false
			return report
		}
	}
	report.Unavailable = true
	return report
}

func isReachableStatus(code int) bool {
	if code <= 0 {
		return false
	}
	// ответ сервера (в т.ч. редирект и страница входа) = хост доступен по этому пути
	if code < 500 {
		return true
	}
	return false
}

func head(ctx context.Context, rawURL, iface string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return 0, err
	}
	client := probeHTTPClient(iface, 5*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		// some servers reject HEAD
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		resp, err = client.Do(req)
		if err != nil {
			return 0, err
		}
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func probeHTTPClient(bindOrProxy string, timeout time.Duration) *http.Client {
	if strings.HasPrefix(bindOrProxy, "http://") || strings.HasPrefix(bindOrProxy, "https://") {
		u, err := url.Parse(bindOrProxy)
		if err == nil {
			return &http.Client{
				Timeout: timeout,
				Transport: &http.Transport{
					Proxy: http.ProxyURL(u),
				},
				CheckRedirect: func(_ *http.Request, via []*http.Request) error {
					if len(via) >= 5 {
						return http.ErrUseLastResponse
					}
					return nil
				},
			}
		}
	}
	return network.HTTPClient(bindOrProxy, timeout)
}

func hostFromURL(raw string) string {
	u := strings.TrimPrefix(raw, "https://")
	u = strings.TrimPrefix(u, "http://")
	if i := strings.Index(u, "/"); i >= 0 {
		u = u[:i]
	}
	if i := strings.Index(u, ":"); i >= 0 {
		u = u[:i]
	}
	return u
}

func DefaultPaths() []config.RoutePath {
	return []config.RoutePath{config.RouteDirect, config.RouteWork, config.RoutePersonal}
}

func BindMap(mainIface, workIface, personalIface string) map[config.RoutePath]string {
	return map[config.RoutePath]string{
		config.RouteDirect:   mainIface,
		config.RouteWork:     workIface,
		config.RoutePersonal: personalIface,
	}
}

func FormatBest(r SiteReport) string {
	if r.Unavailable {
		return "unavailable"
	}
	return fmt.Sprintf("%s", r.BestPath)
}

// ResultForPath возвращает результат по пути или nil.
func ResultForPath(report SiteReport, path config.RoutePath) *PathResult {
	for i := range report.Results {
		if report.Results[i].Path == path {
			return &report.Results[i]
		}
	}
	return nil
}

// BestAvailablePath — первый доступный путь в порядке DefaultPaths.
func BestAvailablePath(report SiteReport) (config.RoutePath, bool) {
	for _, path := range DefaultPaths() {
		r := ResultForPath(report, path)
		if r != nil && r.Available {
			return path, true
		}
	}
	return "", false
}
