package subscription

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxSubscriptionBytes = 8 << 20

// ErrUnsafeSubscriptionURL means a subscription target is not a public HTTP(S) address.
var ErrUnsafeSubscriptionURL = errors.New("подписку можно загружать только с публичных HTTP(S)-адресов; локальные и служебные адреса запрещены")

var errSubscriptionRedirect = errors.New("небезопасное перенаправление подписки или превышен предел 5 переходов")

// Conservative exclusions from the IANA special-purpose registries (2025-10-09).
// IsGlobalUnicast alone also accepts private, shared, documentation and reserved IPs.
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
var subscriptionExcludedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3ffe::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var subscriptionIPv6Unicast = netip.MustParsePrefix("2000::/3")
var subscriptionNAT64 = netip.MustParsePrefix("64:ff9b::/96")

func publicSubscriptionIP(ip netip.Addr) bool {
	if ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	if ip.Is6() {
		if subscriptionNAT64.Contains(ip) {
			// DNS64 is allowed only when its embedded IPv4 is public as well.
			bytes := ip.As16()
			return publicSubscriptionIP(netip.AddrFrom4([4]byte(bytes[12:])))
		}
		if !subscriptionIPv6Unicast.Contains(ip) {
			return false
		}
	}
	for _, prefix := range subscriptionExcludedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func parseFetchURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, ErrUnsafeSubscriptionURL
	}
	if u.Port() != "" {
		port, err := strconv.ParseUint(u.Port(), 10, 16)
		if err != nil || port == 0 {
			return nil, ErrUnsafeSubscriptionURL
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, ErrUnsafeSubscriptionURL
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicSubscriptionIP(ip) {
		return nil, ErrUnsafeSubscriptionURL
	}
	return u, nil
}

func newFetchClient(
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) *http.Client {
	transport := &http.Transport{
		// No environment proxy: it would bypass address validation and DNS pinning.
		Proxy:                  nil,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  15 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			// Transport may detach an in-flight dial from a canceled request for reuse.
			// Bound resolution too, even when no request is still waiting for this dial.
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, ErrUnsafeSubscriptionURL
			}
			var ips []netip.Addr
			if ip, err := netip.ParseAddr(host); err == nil {
				ips = []netip.Addr{ip}
			} else {
				ips, err = lookup(ctx, "ip", host)
				if err != nil {
					return nil, safeFetchError(err)
				}
			}
			if len(ips) == 0 {
				return nil, errors.New("не удалось определить адрес сервера подписки")
			}
			// Reject the entire answer before dialing, including mixed public/private DNS.
			for _, ip := range ips {
				if !publicSubscriptionIP(ip) {
					return nil, ErrUnsafeSubscriptionURL
				}
			}
			for _, ip := range ips {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				// Dial only checked literals. The transport keeps the original Host/TLS name.
				conn, err := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
				if err == nil {
					return conn, nil
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
			}
			return nil, errors.New("не удалось подключиться к серверу подписки")
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return errSubscriptionRedirect
			}
			if _, err := parseFetchURL(req.URL.String()); err != nil {
				return err
			}
			previous := via[len(via)-1].URL
			if previous.Scheme == "https" && req.URL.Scheme != "https" {
				return errSubscriptionRedirect
			}
			// Do not leak tokens from subscription paths/queries through Referer.
			req.Header.Del("Referer")
			// Basic credentials are accepted on the initial URL, never from a redirect.
			req.URL.User = nil
			initial := via[0].URL
			if req.URL.Scheme != initial.Scheme || !strings.EqualFold(req.URL.Host, initial.Host) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

// net/http errors may include the complete subscription URL and its credentials.
// Keep only safe policy/context errors; never return or log the original URL error.
func safeFetchError(err error) error {
	for _, safe := range []error{ErrUnsafeSubscriptionURL, errSubscriptionRedirect, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return fmt.Errorf("время загрузки подписки истекло: %w", context.DeadlineExceeded)
	}
	return errors.New("не удалось загрузить подписку: проверьте адрес и доступность сервера")
}
