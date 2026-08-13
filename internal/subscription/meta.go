package subscription

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Meta — метаданные провайдера из HTTP-заголовков подписки.
type Meta struct {
	HasUserinfo bool
	Upload      int64
	Download    int64
	Total       int64
	ExpireUnix  int64 // 0 = не задан

	ProfileTitle               string
	Announce                   string
	SupportURL                 string
	ProfileUpdateIntervalHours int // 0 = не задан; обычно часы
}

// MetaFromHeaders читает стандартные заголовки Clash / v2rayN / 3x-ui.
func MetaFromHeaders(h http.Header) Meta {
	var m Meta
	if raw := strings.TrimSpace(firstHeader(h, "Subscription-Userinfo")); raw != "" {
		up, down, total, exp, ok := ParseUserinfo(raw)
		if ok {
			m.HasUserinfo = true
			m.Upload = up
			m.Download = down
			m.Total = total
			m.ExpireUnix = exp
		}
	}
	m.ProfileTitle = DecodeHeaderText(firstHeader(h, "Profile-Title"))
	m.Announce = DecodeHeaderText(firstHeader(h, "Announce"))
	m.SupportURL = strings.TrimSpace(firstHeader(h, "Support-Url"))
	if iv := strings.TrimSpace(firstHeader(h, "Profile-Update-Interval")); iv != "" {
		if n, err := strconv.Atoi(iv); err == nil && n > 0 {
			m.ProfileUpdateIntervalHours = n
		}
	}
	return m
}

// DecodeHeaderText снимает префикс base64: у Profile-Title / Announce (Clash/subconverter).
func DecodeHeaderText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	const prefix = "base64:"
	if len(raw) < len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return raw
	}
	payload := strings.TrimSpace(raw[len(prefix):])
	if payload == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(padBase64(payload))
	}
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		return raw
	}
	return strings.TrimSpace(string(decoded))
}

func padBase64(s string) string {
	switch len(s) % 4 {
	case 2:
		return s + "=="
	case 3:
		return s + "="
	default:
		return s
	}
}

// ParseUserinfo разбирает `upload=…; download=…; total=…; expire=…`.
// Неизвестные ключи игнорируются; ok=true если разобран хотя бы один известный ключ.
func ParseUserinfo(raw string) (upload, download, total, expireUnix int64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, 0, 0, false
	}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil || n < 0 {
			continue
		}
		switch key {
		case "upload":
			upload = n
			ok = true
		case "download":
			download = n
			ok = true
		case "total":
			total = n
			ok = true
		case "expire":
			expireUnix = n
			ok = true
		}
	}
	return upload, download, total, expireUnix, ok
}

// ExpireTime — время истечения или zero, если не задано.
func (m Meta) ExpireTime() time.Time {
	if m.ExpireUnix <= 0 {
		return time.Time{}
	}
	return time.Unix(m.ExpireUnix, 0).UTC()
}

func firstHeader(h http.Header, name string) string {
	if h == nil {
		return ""
	}
	return h.Get(name)
}
