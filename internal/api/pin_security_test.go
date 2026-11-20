package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func newPinTestServer(t *testing.T, enabled bool, query string) *Server {
	t.Helper()
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(func(st *config.Settings) {
		st.PersonalVPN.Enabled = enabled
		st.PersonalVPN.ActiveSubscriptionID = "pin-test"
		st.Subscriptions = []config.Subscription{{ID: "pin-test", SelectedNodeID: "pinned"}}
		st.SingBoxPath = filepath.Join(st.DataDir, "nonexistent-sing-box")
	}); err != nil {
		t.Fatal(err)
	}
	st := store.Get()
	if err = subscription.SaveCache(st.DataDir, "pin-test", []subscription.Node{{ID: "pinned", Protocol: "hysteria2", RawURI: "hy2://auth-secret@hy2.example:443?" + query}}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(st.SingBoxConfigPath, []byte("existing working config"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Intentionally no manager: any probe/start/stop/status access would panic.
	// Preflight refusal must return before reaching a runtime dependency.
	return NewTestServer(store, nil)
}

func assertPinTestFilesUnchanged(t *testing.T, s *Server, settingsBefore []byte) {
	t.Helper()
	st := s.Store.Get()
	settingsAfter, err := os.ReadFile(filepath.Join(st.DataDir, "settings.json"))
	if err != nil || !bytes.Equal(settingsBefore, settingsAfter) {
		t.Fatalf("refused pin changed settings: %v", err)
	}
	configAfter, err := os.ReadFile(st.SingBoxConfigPath)
	if err != nil || string(configAfter) != "existing working config" {
		t.Fatalf("refused pin replaced working config: %v", err)
	}
}

func TestPinnedNodePreflightPrecedesRuntimeAccess(t *testing.T) {
	for _, query := range []string{"pinSHA256=pin-secret", "pinSHA256=pin-secret&insecure=1", "pinSHA256=%zz&insecure=1"} {
		t.Run(query, func(t *testing.T) {
			s := newPinTestServer(t, true, query)
			before, err := os.ReadFile(filepath.Join(s.Store.Get().DataDir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.ensureRouterLocked(context.Background()); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "host_not_ready") {
				t.Fatalf("preflight did not refuse safely: %v", err)
			}
			assertPinTestFilesUnchanged(t, s, before)
		})
	}
}

func TestPinnedConnectAndReapplyLeaveRuntimeAndSettingsUntouched(t *testing.T) {
	for _, tc := range []struct {
		path    string
		enabled bool
		status  int
	}{
		{"/api/personal-vpn/connect", false, http.StatusBadRequest},
		{"/api/personal-vpn/connect", true, http.StatusBadRequest},
		{"/api/personal-vpn/reapply", true, http.StatusInternalServerError},
	} {
		t.Run(tc.path, func(t *testing.T) {
			s := newPinTestServer(t, tc.enabled, "pinSHA256=pin-secret&insecure=1")
			before, err := os.ReadFile(filepath.Join(s.Store.Get().DataDir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), "pin сертификата") || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("unsafe API result: %d %s", w.Code, w.Body.String())
			}
			assertPinTestFilesUnchanged(t, s, before)
		})
	}
}

func TestSelectedOutboundPreflightAllowsRegularAndMissingNodes(t *testing.T) {
	s := newPinTestServer(t, false, "sni=hy2.example")
	if err := s.validateSelectedOutbound(s.Store.Get()); err != nil {
		t.Fatalf("ordinary node blocked: %v", err)
	}
	st := s.Store.Get()
	st.PersonalVPN.ActiveSubscriptionID = "missing"
	if err := s.validateSelectedOutbound(st); err != nil {
		t.Fatalf("preflight replaced missing-selection handling: %v", err)
	}
	s = newPinTestServer(t, false, "pinSHA256=pin-secret")
	if err := s.validateSelectedOutbound(s.Store.Get()); !errors.Is(err, singbox.ErrUnsupportedCertificatePin) {
		t.Fatalf("pin detection failed: %v", err)
	}
	if err := s.ensureRouterLocked(context.Background()); err == nil || !strings.Contains(err.Error(), "host_not_ready") {
		t.Fatalf("disabled personal outbound was validated: %v", err)
	}
}
