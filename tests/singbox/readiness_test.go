package singbox_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/singbox"
)

func TestClashReadinessRequiresSuccessfulResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/version" {
					t.Errorf("unexpected readiness path: %s", r.URL.Path)
				}
				w.WriteHeader(status)
			}))
			defer srv.Close()
			deadline := 50 * time.Millisecond
			if status == http.StatusOK {
				deadline = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			err := singbox.WaitClashAPI(ctx, strings.TrimPrefix(srv.URL, "http://"))
			if status == http.StatusOK {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error HTTP status accepted as ready: %v", err)
			}
		})
	}
}
