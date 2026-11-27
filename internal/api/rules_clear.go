package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Valden92/routebox/internal/config"
)

type clearRulesReq struct {
	Source string   `json:"source"`
	IDs    []string `json:"ids"`
}

func (s *Server) clearRules(w http.ResponseWriter, r *http.Request) {
	s.clearRulesWithApply(w, r, s.reapplyPersonalIfRunning)
}

// A single saved mutation and publication; IDs pin the user's confirmed snapshot.
func (s *Server) clearRulesWithApply(w http.ResponseWriter, r *http.Request, apply func(context.Context) (bool, error)) {
	var req clearRulesReq
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_clear_request", "некорректный запрос очистки")
		return
	}
	if dec.Decode(&struct{}{}) != io.EOF || (req.Source != "auto" && req.Source != "manual") || len(req.IDs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid_clear_request", "укажите источник и список подтверждённых правил")
		return
	}
	ids := make(map[string]bool, len(req.IDs))
	for _, id := range req.IDs {
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_clear_request", "пустой идентификатор правила")
			return
		}
		ids[id] = true
	}
	var deleted int
	var conflict bool
	if err := s.Store.Update(func(st *config.Settings) {
		// An automatic rule may have become manual while confirmation was open.
		for _, rule := range st.DomainRules {
			source := "manual"
			if rule.Source == "auto" {
				source = "auto"
			}
			if ids[rule.ID] && source != req.Source {
				conflict = true
				return
			}
		}
		kept := make([]config.DomainRule, 0, len(st.DomainRules))
		for _, rule := range st.DomainRules {
			if ids[rule.ID] {
				deleted++
			} else {
				kept = append(kept, rule)
			}
		}
		st.DomainRules = kept
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "clear_save_failed", "не удалось сохранить очистку правил")
		return
	}
	if conflict {
		writeJSONError(w, http.StatusConflict, "rules_changed", "источник правил изменился; закройте окно и повторите очистку")
		return
	}
	reapplied := false
	if deleted > 0 {
		var err error
		reapplied, err = apply(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "reapply_failed", err.Error())
			return
		}
	}
	writeJSON(w, map[string]any{"status": "deleted", "deleted": deleted, "reapplied": reapplied})
}
