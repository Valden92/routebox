package subscription

import (
	"compress/gzip"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicSubscriptionIP(t *testing.T) {
	for _, address := range []string{
		"0.1.2.3", "10.0.0.1", "100.64.0.1", "100.127.255.254", "127.0.0.1",
		"169.254.169.254", "172.16.0.1", "172.31.255.254", "192.168.1.1",
		"192.0.0.9", "192.0.2.1", "192.88.99.1", "198.18.0.1", "198.19.255.254",
		"198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:100.64.0.1",
		"fc00::1", "fd00::1", "fe80::1", "fec0::1", "ff02::1", "100::1",
		"2001::1", "2001:20::1", "2001:db8::1", "2002:7f00:1::1", "3ffe::1", "3fff::1",
		"5f00::1", "64:ff9b::7f00:1", "64:ff9b::a00:1", "64:ff9b:1::1",
		"2001:4860:4860::8888%eth0",
	} {
		t.Run("blocked_"+address, func(t *testing.T) {
			if publicSubscriptionIP(netip.MustParseAddr(address)) {
				t.Fatal("non-public address accepted")
			}
		})
	}
	for _, address := range []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "100.128.0.1", "172.32.0.1", "::ffff:8.8.8.8", "2606:4700:4700::1111", "2001:4860:4860::8888", "64:ff9b::808:808"} {
		t.Run("allowed_"+address, func(t *testing.T) {
			if !publicSubscriptionIP(netip.MustParseAddr(address)) {
				t.Fatal("public address rejected")
			}
		})
	}
}

// Fake public DNS + an injected dialer redirect sockets only to this test's server.
// Private dependencies stay package-local: production Fetch cannot opt out of its guard.
func fixtureFetchClient(t *testing.T, srv *httptest.Server, lookup func(context.Context, string, string) ([]netip.Addr, error)) *http.Client {
	t.Helper()
	if lookup == nil {
		lookup = func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
	}
	dialer := &net.Dialer{}
	client := newFetchClient(lookup, func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, srv.Listener.Addr().String())
	})
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func fixtureSubscriptionURL(t *testing.T, srv *httptest.Server, host, path string) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Scheme + "://" + net.JoinHostPort(host, u.Port()) + path
}

func TestFetchRejectsUnsafeURLsBeforeNetwork(t *testing.T) {
	for _, address := range []string{
		"file:///etc/passwd", "ftp://example.com/sub", "http:///sub", "http://%zz/secret",
		"http://127.0.0.1/sub", "http://169.254.169.254/latest/meta-data/", "http://192.168.1.1/",
		"http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://[fe80::1%25eth0]/",
		"http://example.com:0/", "http://example.com:65536/", "http://example.com:/",
	} {
		t.Run(address, func(t *testing.T) {
			client := newFetchClient(func(context.Context, string, string) ([]netip.Addr, error) {
				t.Error("unexpected DNS lookup")
				return nil, errors.New("unexpected lookup")
			}, func(context.Context, string, string) (net.Conn, error) {
				t.Error("unexpected connection")
				return nil, errors.New("unexpected dial")
			})
			defer client.CloseIdleConnections()
			if _, err := fetchWithClient(context.Background(), address, client); !errors.Is(err, ErrUnsafeSubscriptionURL) {
				t.Fatalf("expected blocked URL, got %v", err)
			}
		})
	}
}

func TestFetchDNSRejectsPrivateAndMixedAnswers(t *testing.T) {
	for _, addresses := range [][]string{{"127.0.0.1"}, {"::ffff:10.0.0.1"}, {"8.8.8.8", "192.168.1.1"}, {"2606:4700:4700::1111", "fe80::1"}} {
		t.Run(strings.Join(addresses, ","), func(t *testing.T) {
			client := newFetchClient(func(context.Context, string, string) ([]netip.Addr, error) {
				ips := make([]netip.Addr, len(addresses))
				for i, address := range addresses {
					ips[i] = netip.MustParseAddr(address)
				}
				return ips, nil
			}, func(context.Context, string, string) (net.Conn, error) {
				t.Error("must reject all DNS answers before any dial")
				return nil, errors.New("unexpected dial")
			})
			defer client.CloseIdleConnections()
			if _, err := fetchWithClient(context.Background(), "https://subscription.example/secret", client); !errors.Is(err, ErrUnsafeSubscriptionURL) {
				t.Fatalf("expected blocked DNS, got %v", err)
			}
		})
	}
}

func TestFetchPublicSubscriptionPinsDNSAndPreservesMetadata(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != "v2rayN/6.42.0" || r.Header.Get("Accept") != "*/*" || !strings.HasPrefix(r.Host, "subscription.example:") {
			t.Errorf("unexpected provider headers")
		}
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=100; expire=1893456000")
		w.Header().Set("Profile-Title", "Fixture")
		_, _ = io.WriteString(w, "subscription body")
	}))
	defer srv.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	var lookups atomic.Int32
	var mu sync.Mutex
	var dialed []string
	dialer := &net.Dialer{}
	client := newFetchClient(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host != "subscription.example" {
			t.Errorf("proxy or unexpected hostname was resolved: %s", host)
		}
		if lookups.Add(1) > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil // simulated rebinding
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}, func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		return dialer.DialContext(ctx, network, srv.Listener.Addr().String())
	})
	defer client.CloseIdleConnections()
	result, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "subscription.example", "/sub"), client)
	if err != nil || string(result.Body) != "subscription body" || result.Meta.ProfileTitle != "Fixture" || result.Meta.Download != 2 || !result.Meta.HasUserinfo {
		t.Fatalf("body/metadata: %+v, %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if lookups.Load() != 1 || hits.Load() != 1 || len(dialed) != 1 || !strings.HasPrefix(dialed[0], "93.184.216.34:") {
		t.Fatalf("connection was not pinned: DNS=%d dials=%v hits=%d", lookups.Load(), dialed, hits.Load())
	}
}

func TestFetchRedirectsCannotReachPrivateTargets(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1/private", "http://[::1]/private", "http://169.254.169.254/", "http://internal.example/private", "file:///etc/passwd"} {
		t.Run(target, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer srv.Close()
			client := fixtureFetchClient(t, srv, func(_ context.Context, _, host string) ([]netip.Addr, error) {
				if host == "internal.example" {
					return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
				}
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			})
			_, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "subscription.example", "/token-secret?token=secret-value"), client)
			if !errors.Is(err, ErrUnsafeSubscriptionURL) || hits.Load() != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("redirect not safely blocked: hits=%d err=%v", hits.Load(), err)
			}
		})
	}
}

func TestFetchPublicRedirectsStripReferrerAndCrossOriginCredentials(t *testing.T) {
	var hits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/secret-token":
			if r.Header.Get("Authorization") == "" {
				t.Error("initial basic credentials lost")
			}
			http.Redirect(w, r, "/same", http.StatusFound)
		case "/same":
			if r.Referer() != "" || r.Header.Get("Authorization") == "" {
				t.Error("same-origin redirect leaked URL or lost credentials")
			}
			http.Redirect(w, r, strings.Replace(srv.URL, "127.0.0.1", "child.subscription.example", 1)+"/final", http.StatusFound)
		case "/final":
			if r.Referer() != "" || r.Header.Get("Authorization") != "" {
				t.Error("cross-origin redirect leaked URL or credentials")
			}
			_, _ = io.WriteString(w, "ok")
		default:
			t.Error("unexpected path")
		}
	}))
	defer srv.Close()
	client := fixtureFetchClient(t, srv, nil)
	address := strings.Replace(fixtureSubscriptionURL(t, srv, "subscription.example", "/secret-token?key=secret"), "://", "://user:password@", 1)
	got, err := fetchWithClient(context.Background(), address, client)
	if err != nil || string(got.Body) != "ok" || hits.Load() != 3 {
		t.Fatalf("public redirects: body=%s hits=%d err=%v", got.Body, hits.Load(), err)
	}
}

func TestFetchRedirectLimit(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer srv.Close()
	_, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "subscription.example", "/start"), fixtureFetchClient(t, srv, nil))
	if !errors.Is(err, errSubscriptionRedirect) || hits.Load() != 6 {
		t.Fatalf("unbounded redirects: hits=%d err=%v", hits.Load(), err)
	}
}

func TestFetchRechecksDNSOnReconnect(t *testing.T) {
	var hits, lookups atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Connection", "close")
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer srv.Close()
	client := fixtureFetchClient(t, srv, func(context.Context, string, string) ([]netip.Addr, error) {
		if lookups.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	_, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "subscription.example", "/start"), client)
	if !errors.Is(err, ErrUnsafeSubscriptionURL) || lookups.Load() != 2 || hits.Load() != 1 {
		t.Fatalf("DNS rebinding was not blocked: DNS=%d hits=%d err=%v", lookups.Load(), hits.Load(), err)
	}
}

func TestFetchPublicIPFallbackDoesNotResolveLiteralAgain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer srv.Close()
	var attempts atomic.Int32
	dialer := &net.Dialer{}
	client := newFetchClient(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")}, nil
	}, func(ctx context.Context, network, address string) (net.Conn, error) {
		attempts.Add(1)
		if strings.HasPrefix(address, "8.8.8.8:") {
			return nil, errors.New("first address unavailable")
		}
		if !strings.HasPrefix(address, "1.1.1.1:") {
			t.Errorf("unchecked dial: %s", address)
		}
		return dialer.DialContext(ctx, network, srv.Listener.Addr().String())
	})
	defer client.CloseIdleConnections()
	got, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "subscription.example", "/sub"), client)
	if err != nil || string(got.Body) != "ok" || attempts.Load() != 2 {
		t.Fatalf("public fallback: attempts=%d err=%v", attempts.Load(), err)
	}
	client = fixtureFetchClient(t, srv, func(context.Context, string, string) ([]netip.Addr, error) {
		t.Error("unexpected lookup for IP literal")
		return nil, errors.New("unexpected DNS")
	})
	if _, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "1.1.1.1", "/sub"), client); err != nil {
		t.Fatalf("public IP literal: %v", err)
	}
}

func TestFetchLimitsDecompressedBodyAndResponseHeaders(t *testing.T) {
	for _, largeHeaders := range []bool{false, true} {
		t.Run(fmt.Sprint("headers_", largeHeaders), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if largeHeaders {
					w.Header().Set("X-Oversize", strings.Repeat("x", 70<<10))
					return
				}
				w.Header().Set("Content-Encoding", "gzip")
				writer := gzip.NewWriter(w)
				_, _ = io.WriteString(writer, strings.Repeat("x", maxSubscriptionBytes+1))
				_ = writer.Close()
			}))
			defer srv.Close()
			got, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "subscription.example", "/secret-token"), fixtureFetchClient(t, srv, nil))
			if err == nil || len(got.Body) != 0 || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("response not safely bounded: bytes=%d err=%v", len(got.Body), err)
			}
		})
	}
}

func TestFetchTLSChecksOriginalHostnameAndRejectsDowngrade(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.TLS.ServerName != "example.com" {
			t.Errorf("wrong TLS name: %s", r.TLS.ServerName)
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://example.com/sub", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "secure")
	}))
	defer srv.Close()
	client := fixtureFetchClient(t, srv, nil)
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	// Trust only the local test CA. Production uses system roots and hostname checks.
	client.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	got, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "example.com", "/sub"), client)
	if err != nil || string(got.Body) != "secure" {
		t.Fatalf("valid TLS: %v", err)
	}
	if _, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "wrong.example", "/sub"), client); err == nil {
		t.Fatal("TLS hostname verification bypassed")
	}
	if _, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "example.com", "/redirect"), client); !errors.Is(err, errSubscriptionRedirect) {
		t.Fatalf("HTTPS downgrade not blocked: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("unexpected requests after TLS failure/downgrade: %d", hits.Load())
	}
}

func TestFetchBodyLimitAndHTTPFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		size      int
		status    int
		wantError bool
	}{
		{"at_limit", maxSubscriptionBytes, http.StatusOK, false},
		{"over_limit", maxSubscriptionBytes + 1, http.StatusOK, true},
		{"http_error", 0, http.StatusForbidden, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, strings.Repeat("x", tc.size))
			}))
			defer srv.Close()
			got, err := fetchWithClient(context.Background(), fixtureSubscriptionURL(t, srv, "subscription.example", "/secret"), fixtureFetchClient(t, srv, nil))
			if (err != nil) != tc.wantError || (err == nil && len(got.Body) != tc.size) || (err != nil && len(got.Body) != 0) {
				t.Fatalf("body=%d error=%v", len(got.Body), err)
			}
		})
	}
}

func TestFetchCancellationTimeoutAndSafeErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	client := fixtureFetchClient(t, srv, nil)
	client.Timeout = 50 * time.Millisecond
	address := fixtureSubscriptionURL(t, srv, "subscription.example", "/secret-token")
	if _, err := fetchWithClient(context.Background(), address, client); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body timeout not enforced: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetchWithClient(ctx, address, client); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	for _, cause := range []error{errors.New("secret-token"), &url.Error{URL: "https://user:password@provider/secret-token", Err: errors.New("secret-token")}} {
		err := safeFetchError(cause)
		if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "password") {
			t.Fatalf("secret in error: %v", err)
		}
	}
	for _, answer := range []struct {
		ips []netip.Addr
		err error
	}{{nil, nil}, {nil, fmt.Errorf("resolver secret-token")}} {
		client := newFetchClient(func(context.Context, string, string) ([]netip.Addr, error) { return answer.ips, answer.err }, func(context.Context, string, string) (net.Conn, error) {
			t.Error("dial on failed/empty DNS answer")
			return nil, errors.New("secret-token")
		})
		_, err := fetchWithClient(context.Background(), address, client)
		client.CloseIdleConnections()
		if err == nil || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("unsafe DNS failure: %v", err)
		}
	}
}
