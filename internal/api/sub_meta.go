package api

import (
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/subscription"
)

// applySubscriptionMeta обновляет поля подписки из HTTP-заголовков fetch.
// Пустые/отсутствующие заголовки не затирают уже сохранённые значения
// (кроме Userinfo целиком, если заголовок был).
func applySubscriptionMeta(sub *config.Subscription, m subscription.Meta) {
	if sub == nil {
		return
	}
	if m.HasUserinfo {
		sub.TrafficUpload = m.Upload
		sub.TrafficDownload = m.Download
		sub.TrafficTotal = m.Total
		sub.ExpireAt = m.ExpireTime()
	}
	if m.ProfileTitle != "" {
		sub.ProfileTitle = m.ProfileTitle
	}
	if m.Announce != "" {
		sub.Announce = m.Announce
	}
	if m.SupportURL != "" {
		sub.SupportURL = m.SupportURL
	}
	if m.ProfileUpdateIntervalHours > 0 {
		sub.ProfileUpdateIntervalHours = m.ProfileUpdateIntervalHours
	}
}
