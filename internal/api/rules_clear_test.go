package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
)

func TestClearRulesPublishesOnceAfterSavingAndReportsApplyFailure(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "publication failure"}[fails], func(t *testing.T) {
			store, err := config.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Update(func(st *config.Settings) {
				st.DomainRules = []config.DomainRule{{ID: "1", Source: "auto"}, {ID: "2", Source: "auto"}}
			}); err != nil {
				t.Fatal(err)
			}
			srv := &Server{Store: store}
			calls := 0
			apply := func(context.Context) (bool, error) {
				calls++
				if len(store.Get().DomainRules) != 0 {
					t.Fatal("publication preceded saving")
				}
				if fails {
					return false, errors.New("fixture apply failure")
				}
				return true, nil
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/rules/clear", strings.NewReader(`{"source":"auto","ids":["1","2"]}`))
			srv.clearRulesWithApply(w, req, apply)
			want := 200
			if fails {
				want = 500
			}
			if w.Code != want || calls != 1 || len(store.Get().DomainRules) != 0 {
				t.Fatalf("status=%d calls=%d", w.Code, calls)
			}
			if fails && !strings.Contains(w.Body.String(), "reapply_failed") {
				t.Fatal("persisted failure not distinguished")
			}
			w = httptest.NewRecorder()
			req = httptest.NewRequest("POST", "/api/rules/clear", strings.NewReader(`{"source":"auto","ids":["1","2"]}`))
			srv.clearRulesWithApply(w, req, apply)
			if w.Code != 200 || calls != 1 {
				t.Fatal("empty cleanup republished rules")
			}
		})
	}
}
