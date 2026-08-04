package api

import (
	"encoding/json"
	"net/http"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

type PersonalReadiness struct {
	Configured bool   `json:"configured"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
}

func activeSubscription(st config.Settings) *config.Subscription {
	id := st.PersonalVPN.ActiveSubscriptionID
	if id == "" {
		return nil
	}
	for i := range st.Subscriptions {
		if st.Subscriptions[i].ID == id {
			return &st.Subscriptions[i]
		}
	}
	return nil
}

func (s *Server) personalReadiness() PersonalReadiness {
	st := s.Store.Get()
	if !singbox.HostReady(st.SingBoxPath) {
		return PersonalReadiness{
			Configured: false,
			Reason:     "host_not_ready",
			Message:    "Выполните в терминале: make sync (настройка TUN и NetworkManager)",
		}
	}
	if len(st.Subscriptions) == 0 {
		return PersonalReadiness{
			Configured: false,
			Reason:     "no_subscriptions",
			Message:    "Добавьте подписку на вкладке «Подписки»",
		}
	}
	if st.PersonalVPN.ActiveSubscriptionID == "" {
		return PersonalReadiness{
			Configured: false,
			Reason:     "no_active_subscription",
			Message:    "Откройте подписку, обновите список серверов и выберите узел",
		}
	}
	sub := activeSubscription(st)
	if sub == nil {
		return PersonalReadiness{
			Configured: false,
			Reason:     "no_active_subscription",
			Message:    "Подписка не найдена — выберите подписку заново",
		}
	}
	c, err := subscription.LoadCache(st.DataDir, sub.ID)
	if err != nil || len(c.Nodes) == 0 {
		return PersonalReadiness{
			Configured: false,
			Reason:     "no_nodes",
			Message:    "Нажмите «Серверы» → «Обновить подписку», затем выберите сервер",
		}
	}
	if sub.SelectedNodeID == "" {
		return PersonalReadiness{
			Configured: false,
			Reason:     "no_server",
			Message:    "Выберите сервер в списке узлов",
		}
	}
	if _, ok := subscription.FindNodeByID(c.Nodes, sub.SelectedNodeID); !ok {
		return PersonalReadiness{
			Configured: false,
			Reason:     "server_missing",
			Message:    "Выбранный сервер исчез из подписки — откройте список и выберите другой",
		}
	}
	return PersonalReadiness{Configured: true, Reason: "ok"}
}

func (s *Server) personalReadinessHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.personalReadiness())
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}
