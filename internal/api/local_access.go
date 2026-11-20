package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LocalAccess is the outer HTTP boundary, before the router or any state changes.
// The credential is read by the native UI, never served by the daemon over HTTP.
type LocalAccess struct{ token string }

func NewLocalAccess(credentialDir string) (*LocalAccess, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(secret)
	if err := os.MkdirAll(credentialDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(credentialDir, ".api-token-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(token); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(f.Name(), filepath.Join(credentialDir, ".api-token")); err != nil {
		return nil, err
	}
	return &LocalAccess{token: token}, nil
}

// ValidateListenAddress rejects wildcard and external binds before startup touches settings.
func ValidateListenAddress(address string) error {
	if !localAddress(address) {
		return fmt.Errorf("API listen address must be loopback with an explicit port")
	}
	return nil
}

func localAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func allowedOrigin(origin string) bool {
	switch origin {
	case "http://localhost:1420", "http://127.0.0.1:1420", "tauri://localhost", "http://tauri.localhost", "https://tauri.localhost":
		return true
	default:
		return false
	}
}

func allowedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (a *LocalAccess) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Origin")
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(peer)
		if err != nil || ip == nil || !ip.IsLoopback() || !localAddress(r.Host) {
			writeJSONError(w, http.StatusForbidden, "local_api_only", "API доступен только через локальный адрес")
			return
		}
		origins := r.Header.Values("Origin")
		origin := r.Header.Get("Origin")
		if len(origins) > 1 || (len(origins) != 0 && !allowedOrigin(origin)) {
			writeJSONError(w, http.StatusForbidden, "origin_not_allowed", "Этому источнику запрещён доступ к API")
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions {
			if origin == "" || !allowedMethod(r.Header.Get("Access-Control-Request-Method")) {
				writeJSONError(w, http.StatusForbidden, "preflight_not_allowed", "Запрос не разрешён")
				return
			}
			for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				switch strings.ToLower(strings.TrimSpace(header)) {
				case "", "authorization", "content-type":
				default:
					writeJSONError(w, http.StatusForbidden, "preflight_not_allowed", "Заголовок не разрешён")
					return
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		public := r.Method == http.MethodGet && (r.URL.Path == "/api/health" || r.URL.Path == "/api/version")
		if !public {
			auth := r.Header.Values("Authorization")
			expected := "Bearer " + a.token
			if len(auth) != 1 || subtle.ConstantTimeCompare([]byte(auth[0]), []byte(expected)) != 1 {
				writeJSONError(w, http.StatusUnauthorized, "api_auth_required", "Требуется авторизация локального клиента")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
