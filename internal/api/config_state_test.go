package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Valden92/routebox/internal/config"
)

func applyStateFixture(t *testing.T) (*rollbackFixture, *configApplyTracker, config.Settings) {
	t.Helper()
	f := newRollbackFixture(t, false, false)
	tracker := &configApplyTracker{}
	st := config.DefaultSettings(t.TempDir())
	if err := tracker.apply(context.Background(), st, f.path, "startup", prepareRollbackFixture, f.runtime); err != nil {
		t.Fatal(err)
	}
	return f, tracker, st
}

func assertAppliedState(t *testing.T, state routerConfigurationState, version string) {
	t.Helper()
	if state.AppliedVersion != version || state.LastSuccessfulVersion != version || state.StartedAt == nil || state.CompletedAt == nil || state.LastSuccessfulAt == nil {
		t.Fatalf("incorrect versions/timestamps: %+v", state)
	}
}

func TestConfigStateTracksConfirmedApplyAndSavedChanges(t *testing.T) {
	f, tracker, st := applyStateFixture(t)
	state := tracker.snapshot(st, f.alive)
	assertAppliedState(t, state, configVersion([]byte("new")))
	if state.Attempt != 1 || state.Phase != "applied" || state.Reason != "startup" || state.MatchesSavedSettings == nil || !*state.MatchesSavedSettings {
		t.Fatalf("incorrect confirmed state: %+v", state)
	}
	// A later saved rule is not silently presented as applied.
	st.DomainRules = []config.DomainRule{{Pattern: "new.test", Path: config.RoutePersonal, Enabled: true}}
	state = tracker.snapshot(st, true)
	if state.MatchesSavedSettings == nil || *state.MatchesSavedSettings || state.AppliedVersion != configVersion([]byte("new")) {
		t.Fatalf("saved changes presented as applied: %+v", state)
	}
	state = tracker.snapshot(st, false)
	if state.AppliedVersion != "" || state.MatchesSavedSettings != nil || state.LastSuccessfulVersion == "" {
		t.Fatalf("stopped router presented as active: %+v", state)
	}
}

func TestConfigStateDistinguishesRejectionAndRecovery(t *testing.T) {
	for _, outcome := range []string{"rejected", "rolled_back", "rollback_failed", "preserved"} {
		t.Run(outcome, func(t *testing.T) {
			f, tracker, st := applyStateFixture(t)
			prepare := func(path string) error { return os.WriteFile(path, []byte("next"), 0o600) }
			failure := errors.New("private URI hy2://password@provider.test and /private/path")
			switch outcome {
			case "rejected":
				prepare = func(string) error { return failure }
			case "rolled_back":
				f.startErrors[2] = failure
			case "rollback_failed":
				f.startErrors[2], f.startErrors[3] = failure, failure
			case "preserved":
				f.stopErrors[1] = failure
			}
			st.MainInterface = "changed-main"
			err := tracker.apply(context.Background(), st, f.path, "rules_changed", prepare, f.runtime)
			if !errors.Is(err, failure) {
				t.Fatalf("original error chain lost: %v", err)
			}
			state := tracker.snapshot(st, f.alive)
			want := outcome
			if want == "preserved" {
				want = "rejected"
			}
			if state.Phase != want || state.Attempt != 2 || state.Error == "" || state.LastSuccessfulVersion != configVersion([]byte("new")) {
				t.Fatalf("incorrect failure state: %+v", state)
			}
			body, marshalErr := json.Marshal(state)
			if marshalErr != nil || strings.Contains(string(body), "password") || strings.Contains(string(body), "/private/path") || strings.Contains(string(body), "hy2://") {
				t.Fatalf("diagnostic leaked an original error: %s, %v", body, marshalErr)
			}
			if outcome == "rollback_failed" {
				if state.AppliedVersion != "" || state.MatchesSavedSettings != nil {
					t.Fatal("failed recovery advertised a confirmed process")
				}
			} else {
				assertAppliedState(t, state, configVersion([]byte("new")))
				if state.MatchesSavedSettings == nil || *state.MatchesSavedSettings {
					t.Fatal("failed candidate was presented as matching saved settings")
				}
			}
		})
	}
}

func TestConfigStateFirstFailureDoesNotInventAppliedVersion(t *testing.T) {
	f := newRollbackFixture(t, false, false)
	f.startErrors[1] = errors.New("fixture start failure")
	var tracker configApplyTracker
	st := config.DefaultSettings(t.TempDir())
	err := tracker.apply(context.Background(), st, f.path, "startup", prepareRollbackFixture, f.runtime)
	state := tracker.snapshot(st, f.alive)
	if err == nil || state.Phase != "failed" || state.Rollback != "file_restored" || state.AppliedVersion != "" || state.LastSuccessfulVersion != "" {
		t.Fatalf("invented working config: %+v, %v", state, err)
	}
}

func TestRollbackOfUntrackedVersionDoesNotGuessItsSettings(t *testing.T) {
	f := newRollbackFixture(t, true, true)
	f.startErrors[1] = errors.New("fixture candidate failure")
	var tracker configApplyTracker
	st := config.DefaultSettings(t.TempDir())
	initial := tracker.snapshot(st, true)
	if initial.Phase != "unknown" || initial.AppliedVersion != "" || initial.MatchesSavedSettings != nil {
		t.Fatal("existing process was assumed to use saved settings")
	}
	if err := tracker.apply(context.Background(), st, f.path, "manual_reapply", prepareRollbackFixture, f.runtime); err == nil {
		t.Fatal("candidate unexpectedly succeeded")
	}
	state := tracker.snapshot(st, f.alive)
	if state.Phase != "rolled_back" || state.AppliedVersion != configVersion([]byte("old")) || state.MatchesSavedSettings != nil {
		t.Fatalf("restored file was assumed to match current settings: %+v", state)
	}
}

func TestConfigStateNeverConfirmsCandidateBeforeReadiness(t *testing.T) {
	f, tracker, st := applyStateFixture(t)
	started, resume := make(chan struct{}), make(chan struct{})
	f.onStart = func(int) {
		close(started)
		<-resume
	}
	done := make(chan error, 1)
	go func() {
		done <- tracker.apply(context.Background(), st, f.path, "manual_reapply", prepareRollbackFixture, f.runtime)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture did not reach start")
	}
	// Even an alive new process is not confirmed until the ready callback succeeds.
	state := tracker.snapshot(st, true)
	if state.Phase != "applying" || state.AppliedVersion != "" || state.MatchesSavedSettings != nil || state.LastSuccessfulVersion == "" {
		t.Errorf("unconfirmed candidate presented as active: %+v", state)
	}
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConfigStateGetterDoesNoPreparationOrHostWork(t *testing.T) {
	s := routingSettingsFixture(t, false)
	before := s.Store.Get()
	for range 2 {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/router/configuration", nil))
		var state routerConfigurationState
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &state) != nil || state.Attempt != 0 || state.Phase != "unknown" {
			t.Fatalf("initial state: %d %s", rec.Code, rec.Body.String())
		}
	}
	if got := s.Store.Get(); len(got.DomainRules) != len(before.DomainRules) || s.configApply.state.Attempt != 0 {
		t.Fatal("diagnostics changed configuration")
	}
}

func TestRoutingSettingsKeyExcludesLearningAndSubscriptionMetadata(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	st.PersonalVPN.ActiveSubscriptionID = "active"
	st.Subscriptions = []config.Subscription{{ID: "active", SelectedNodeID: "node", Name: "before"}}
	st.DomainRules = []config.DomainRule{{ID: "rule", Pattern: "example.test", Path: config.RoutePersonal, Source: "manual", Enabled: true}}
	key := routingSettingsKey(st)
	st.DomainRules[0].CheckCount++
	st.DomainRules[0].LastCheckedAt = time.Now()
	st.DomainRules[0].Source = ""
	st.Subscriptions[0].Name = "after"
	st.Subscriptions[0].NodeSelectCounts = map[string]int{"node": 10}
	st.AutoDetectRouting = false
	if routingSettingsKey(st) != key {
		t.Fatal("metadata or learning toggle marked routes as unapplied")
	}
	st.Subscriptions[0].SelectedNodeID = "another-node"
	if routingSettingsKey(st) == key {
		t.Fatal("new server selection was ignored")
	}
}

func TestConfigurationApplyReasonUsesSafeCategories(t *testing.T) {
	for _, tc := range []struct{ pattern, want string }{
		{"/api/personal-vpn/connect", "personal_enabled"},
		{"/api/personal-vpn/disconnect", "personal_disabled"},
		{"/api/personal-vpn/reapply", "manual_reapply"},
		{"/api/rules/{id}", "rules_changed"},
		{"/api/subscriptions/{id}/select", "subscription_changed"},
		{"/unknown/private/path", "configuration_change"},
	} {
		route := chi.NewRouteContext()
		route.RoutePatterns = []string{tc.pattern}
		ctx := context.WithValue(context.Background(), chi.RouteCtxKey, route)
		if got := configurationApplyReason(ctx); got != tc.want {
			t.Fatalf("%s: reason=%s", tc.pattern, got)
		}
	}
	if got := configurationApplyReason(withApplyReason(context.Background(), "system_vpn_changed")); got != "system_vpn_changed" {
		t.Fatal("background reason lost")
	}
}
