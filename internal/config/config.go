package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const AppName = "vpn-router"

type RoutePath string

const (
	RouteDirect   RoutePath = "direct"
	RouteWork     RoutePath = "work"
	RoutePersonal RoutePath = "personal"
)

type DomainRule struct {
	ID            string    `json:"id"`
	Pattern       string    `json:"pattern"`
	Path          RoutePath `json:"path"`
	Source        string    `json:"source"` // manual, auto, import
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"createdAt"`
	LastCheckedAt time.Time `json:"lastCheckedAt,omitempty"`
	LastSuccessAt time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
	CheckCount    int       `json:"checkCount,omitempty"`
}

type AppRule struct {
	ID          string    `json:"id"`
	ProcessName string    `json:"processName"`
	ExecPath    string    `json:"execPath,omitempty"`
	Path        RoutePath `json:"path"`
	Enabled     bool      `json:"enabled"`
}

type Subscription struct {
	ID                     string         `json:"id"`
	Name                   string         `json:"name"`
	URL                    string         `json:"url"`
	Source                 string         `json:"source,omitempty"`       // url|text|uri|ovpn|clash
	RefreshIntervalMinutes int            `json:"refreshIntervalMinutes"` // при autoRefresh; 0 = только вручную
	AutoRefresh            bool           `json:"autoRefresh"`
	CreatedAt              time.Time      `json:"createdAt,omitempty"`
	LastRefresh            time.Time      `json:"lastRefresh,omitempty"`
	SelectedNodeID         string         `json:"selectedNodeId,omitempty"`
	NodeSelectCounts       map[string]int `json:"nodeSelectCounts,omitempty"` // nodeId → число выборов
	Enabled                bool           `json:"enabled"`
	// Метаданные провайдера (Subscription-Userinfo и др.) после fetch URL.
	TrafficUpload              int64     `json:"trafficUpload,omitempty"`
	TrafficDownload            int64     `json:"trafficDownload,omitempty"`
	TrafficTotal               int64     `json:"trafficTotal,omitempty"`
	ExpireAt                   time.Time `json:"expireAt,omitempty"`
	ProfileTitle               string    `json:"profileTitle,omitempty"`
	Announce                   string    `json:"announce,omitempty"`
	SupportURL                 string    `json:"supportUrl,omitempty"`
	ProfileUpdateIntervalHours int       `json:"profileUpdateIntervalHours,omitempty"`
	// ImportSummary — для локального импорта (.ovpn и т.п.), без HTTP-заголовков.
	ImportSummary string `json:"importSummary,omitempty"`
	// NodeCount — только в ответах API (из кэша узлов), в settings не хранится осмысленно.
	NodeCount int `json:"nodeCount,omitempty"`
}

func (s Subscription) RefreshInterval() time.Duration {
	if s.RefreshIntervalMinutes <= 0 {
		return 0
	}
	return time.Duration(s.RefreshIntervalMinutes) * time.Minute
}

// SystemVPN — профиль NetworkManager, который отслеживаем (управление только из ОС).
type SystemVPN struct {
	NMConnectionID string `json:"nmConnectionId"`
}

type PersonalVPN struct {
	ActiveSubscriptionID string `json:"activeSubscriptionId"`
	AutoConnect          bool   `json:"autoConnect"`
	Enabled              bool   `json:"enabled"`
}

type Settings struct {
	DataDir           string         `json:"-"`
	MainInterface     string         `json:"mainInterface"`
	SystemVPN         SystemVPN      `json:"systemVpn"`
	PersonalVPN       PersonalVPN    `json:"personalVpn"`
	Subscriptions     []Subscription `json:"subscriptions"`
	DomainRules       []DomainRule   `json:"domainRules"`
	AppRules          []AppRule      `json:"appRules"`
	DefaultPath       RoutePath      `json:"defaultPath"`
	AutoDetectRouting bool           `json:"autoDetectRouting"`
	SingBoxPath       string         `json:"singBoxPath"`
	SingBoxConfigPath string         `json:"singBoxConfigPath"`
	APIListen         string         `json:"apiListen"`
	Encrypted         bool           `json:"encrypted"`
}

type Store struct {
	mu       sync.RWMutex
	settings Settings
	path     string
}

func DefaultSettings(dataDir string) Settings {
	return Settings{
		DataDir:           dataDir,
		MainInterface:     "wlp0s20f3",
		APIListen:         "127.0.0.1:47891",
		DefaultPath:       RoutePersonal,
		AutoDetectRouting: true,
		SystemVPN: SystemVPN{
			NMConnectionID: "PTsecurity",
		},
		PersonalVPN: PersonalVPN{
			AutoConnect: false,
		},
		SingBoxPath:       defaultSingBoxPath(),
		SingBoxConfigPath: filepath.Join(dataDir, "sing-box.json"),
	}
}

func defaultSingBoxPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "sing-box"
	}
	p := filepath.Join(home, ".local", "bin", "sing-box")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return "sing-box"
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		settings: DefaultSettings(dataDir),
		path:     filepath.Join(dataDir, "settings.json"),
	}
	if err := s.Load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) Update(fn func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.settings)
	return s.saveLocked()
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var st Settings
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	if st.SystemVPN.NMConnectionID == "" {
		var legacy struct {
			WorkVPN SystemVPN `json:"workVpn"`
		}
		if json.Unmarshal(b, &legacy) == nil && legacy.WorkVPN.NMConnectionID != "" {
			st.SystemVPN = legacy.WorkVPN
		}
	}
	st.DataDir = s.settings.DataDir
	if st.APIListen == "" {
		st.APIListen = s.settings.APIListen
	}
	if st.SingBoxConfigPath == "" {
		st.SingBoxConfigPath = filepath.Join(st.DataDir, "sing-box.json")
	}
	if st.SingBoxPath == "" {
		st.SingBoxPath = defaultSingBoxPath()
	}
	s.settings = st
	return nil
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

func (s *Store) SavePlain() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}
