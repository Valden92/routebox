package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/routing"
)

// Versions describe generated config bytes, never settings or credentials.
type routerConfigurationState struct {
	Attempt               uint64     `json:"attempt"`
	Phase                 string     `json:"phase"`
	Reason                string     `json:"reason,omitempty"`
	DesiredVersion        string     `json:"desiredVersion,omitempty"`
	AppliedVersion        string     `json:"appliedVersion,omitempty"`
	LastSuccessfulVersion string     `json:"lastSuccessfulVersion,omitempty"`
	StartedAt             *time.Time `json:"startedAt,omitempty"`
	CompletedAt           *time.Time `json:"completedAt,omitempty"`
	LastSuccessfulAt      *time.Time `json:"lastSuccessfulAt,omitempty"`
	Rollback              string     `json:"rollback,omitempty"`
	Error                 string     `json:"error,omitempty"`
	Running               bool       `json:"running"`
	MatchesSavedSettings  *bool      `json:"matchesSavedSettings,omitempty"`
	PublishedRulesVersion string     `json:"publishedRulesVersion,omitempty"`
	Unchanged             bool       `json:"unchanged,omitempty"`
}

type configApplyTracker struct {
	mu            sync.RWMutex
	state         routerConfigurationState
	appliedKey    [32]byte
	settingsKnown bool
}

// Machine-readable recovery outcome; retain existing error text and errors.Is.
type routerApplyError struct {
	cause           error
	rollback        string
	restoredVersion string
}

func (e *routerApplyError) Error() string { return e.cause.Error() }
func (e *routerApplyError) Unwrap() error { return e.cause }

func configVersion(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (t *configApplyTracker) apply(ctx context.Context, st config.Settings, path, reason string, prepare func(string) error, runtime routerRuntime) (bool, error) {
	t.mu.Lock()
	confirmedVersion := t.state.AppliedVersion
	t.state.Attempt++
	now := time.Now()
	t.state.Phase, t.state.Reason = "preparing", reason
	t.state.DesiredVersion, t.state.Rollback, t.state.Error = "", "", ""
	t.state.Unchanged = false
	t.state.StartedAt, t.state.CompletedAt = &now, nil
	t.mu.Unlock()
	runtime.reuse = func(body []byte) bool {
		return reason != "manual_reapply" && confirmedVersion != "" && configVersion(body) == confirmedVersion
	}
	runtime.progress = func(phase string) {
		t.mu.Lock()
		t.state.Phase = phase
		t.mu.Unlock()
	}
	err := applyRouterConfig(ctx, path, func(candidate string) error {
		prepareErr := prepare(candidate)
		// Observation cannot turn a successful preparation into a failed apply.
		if body, readErr := os.ReadFile(candidate); readErr == nil && len(body) > 0 {
			t.mu.Lock()
			t.state.DesiredVersion = configVersion(body)
			t.mu.Unlock()
		}
		return prepareErr
	}, runtime)
	t.finish(err, routingSettingsKey(st))
	if errors.Is(err, errConfigUnchanged) {
		return false, nil
	}
	return err == nil, err
}

func (t *configApplyTracker) finish(err error, key [32]byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.state.CompletedAt = &now
	if errors.Is(err, errConfigUnchanged) {
		t.state.Phase, t.state.Unchanged = "applied", true
		t.appliedKey, t.settingsKnown = key, true
		return
	}
	if err == nil {
		t.state.PublishedRulesVersion = ""
		t.state.Phase = "applied"
		t.state.AppliedVersion = t.state.DesiredVersion
		if t.state.AppliedVersion != "" {
			t.state.LastSuccessfulVersion = t.state.AppliedVersion
			t.state.LastSuccessfulAt = &now
		}
		t.appliedKey, t.settingsKnown = key, t.state.AppliedVersion != ""
		return
	}
	// Do not copy core diagnostics: they can contain credentials or private paths.
	t.state.Error = "Новая конфигурация не прошла подготовку или проверку; рабочий процесс не заменён."
	t.state.Phase = "rejected"
	var failure *routerApplyError
	if !errors.As(err, &failure) {
		return
	}
	t.state.Rollback = failure.rollback
	switch failure.rollback {
	case "restored":
		t.state.Phase = "rolled_back"
		t.state.Error = "Применение не удалось; предыдущая рабочая конфигурация и процесс восстановлены."
		t.settingsKnown = t.settingsKnown && t.state.LastSuccessfulVersion == failure.restoredVersion
		t.state.AppliedVersion, t.state.LastSuccessfulVersion = failure.restoredVersion, failure.restoredVersion
		t.state.LastSuccessfulAt = &now
	case "preserved":
		t.state.Error = "Рабочий процесс не заменён; предыдущий файл конфигурации восстановлен."
	case "file_restored":
		t.state.Phase = "failed"
		t.state.AppliedVersion = ""
		t.settingsKnown = false
		t.state.Error = "Запуск не удался; файл восстановлен, ранее работающего маршрутизатора не было."
	default:
		t.state.Phase = "rollback_failed"
		t.state.AppliedVersion = ""
		t.settingsKnown = false
		t.state.Error = "Применение и восстановление не завершились успешно. Проверьте состояние сети и журнал приложения."
	}
}

func (t *configApplyTracker) snapshot(st config.Settings, running bool) routerConfigurationState {
	key := routingSettingsKey(st)
	t.mu.RLock()
	defer t.mu.RUnlock()
	state := t.state
	state.Running = running
	if state.Phase == "" {
		state.Phase = "unknown"
	}
	if !running || state.Phase == "applying" || state.Phase == "rolling_back" {
		state.AppliedVersion = ""
	} else if t.settingsKnown && state.AppliedVersion != "" {
		matches := t.appliedKey == key
		state.MatchesSavedSettings = &matches
	}
	return state
}

// Ignore learning statistics, labels and other fields not used to generate routes.
// Node-cache and OS network changes are verified by the next candidate generation.
func routingSettingsKey(st config.Settings) [32]byte {
	type domain struct {
		Pattern string
		Path    config.RoutePath
	}
	type app struct {
		Process string
		Path    config.RoutePath
	}
	var enabled []config.DomainRule
	for _, r := range st.DomainRules {
		if r.Enabled {
			enabled = append(enabled, r)
		}
	}
	var domains []domain
	for _, r := range routing.CollapseDomainRules(enabled) {
		domains = append(domains, domain{r.Pattern, r.Path})
	}
	var apps []app
	for _, r := range st.AppRules {
		if r.Enabled && r.ProcessName != "" {
			apps = append(apps, app{r.ProcessName, r.Path})
		}
	}
	selected := ""
	if sub := activeSubscription(st); sub != nil {
		selected = sub.SelectedNodeID
	}
	// Only bool/string/slices of these types: encoding cannot fail.
	body, _ := json.Marshal(struct {
		Main, System, Subscription, Node string
		Personal, Fragment               bool
		Domains                          []domain
		Apps                             []app
	}{st.MainInterface, st.SystemVPN.NMConnectionID, st.PersonalVPN.ActiveSubscriptionID, selected, st.PersonalVPN.Enabled, st.PersonalVPN.FragmentTLS(), domains, apps})
	return sha256.Sum256(body)
}

type applyReasonKey struct{}

func withApplyReason(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, applyReasonKey{}, reason)
}

func configurationApplyReason(ctx context.Context) string {
	if reason, ok := ctx.Value(applyReasonKey{}).(string); ok {
		return reason
	}
	if route := chi.RouteContext(ctx); route != nil {
		pattern := route.RoutePattern()
		switch {
		case strings.HasPrefix(pattern, "/api/rules"):
			return "rules_changed"
		case strings.HasPrefix(pattern, "/api/subscriptions"):
			return "subscription_changed"
		case pattern == "/api/personal-vpn/connect":
			return "personal_enabled"
		case pattern == "/api/personal-vpn/disconnect":
			return "personal_disabled"
		case pattern == "/api/personal-vpn/reapply":
			return "manual_reapply"
		}
	}
	return "configuration_change"
}

func (s *Server) routerConfiguration(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.configApply.snapshot(s.Store.Get(), s.SingBox.Running()))
}
