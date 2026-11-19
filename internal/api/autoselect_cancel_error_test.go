package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutoSelectMissingJobHasStableErrorCode(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.autoSelectNodeCancel(w, autoSelectCancelRequest(t, "sub", "expired"))
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusNotFound || body.Code != "auto_select_job_not_found" {
		t.Fatalf("client cannot distinguish an expired job from an unsupported cancel API: %d %s", w.Code, w.Body.String())
	}
}
