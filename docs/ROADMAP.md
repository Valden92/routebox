# Дорожная карта доработок — Router BOX

Единый бэклог по приоритетам. Источники:

- `README.md` (раздел «Дорожная карта»)
- `docs/research/3x-ui-client-ideas.md` (клиентские идеи из 3x-ui)
- решения по продукту (этот файл — канон; новые идеи сначала сюда)

**Оценка сложности:** S (дни) · M (около недели) · L (заметно дольше / несколько подсистем).  
**Правило:** новые идеи сначала в этот файл (или research → сюда), не только в чат.

Подробности реализации по пунктам из 3x-ui — в `docs/research/3x-ui-client-ideas.md` (§3).

---

## Сделано (ядро)

| Что | Примечание |
|-----|------------|
| Политический маршрут sing-box + coexist с `tun0` | |
| Always-on маршрутизатор + enable/disable личного outbound | |
| Автоправила доменов и UI правил | |
| Наблюдение трафика / истории без лишних MFA-проб corp | |
| Импорт OpenVPN `.ovpn` (sing-box ≥ 1.14) | |
| Фикс применения доменных правил (reapply, dedupe, DNS proxy) | |
| TUN perf: `stack: system`, MTU 1400, `auto_redirect` | Nested OpenVPN всё ещё медленнее нативного клиента |
| UI «Наблюдение» → Активно / Не активно | |
| Заголовки подписки (Userinfo / Profile-*) | Квота и срок на карточке после fetch/refresh |
| Карточка подписки: тип, даты, квота, «Выбрана» / «Не выбрана» | |
| Явная активация подписки (`POST …/activate`) | Выбор сервера по-прежнему тоже активирует |
| Автовыбор единственного сервера | Add / refresh / list heal через `RemapSelection` |
| Clash / Mihomo YAML import | proxies → rawUri; вставка в «Текст»/файл/URL с автодетектом |
| QR import (картинка / буфер) | Без камеры: файл или Ctrl+V → classify → url/uri/text |

---

## Бэклог по приоритетам

| Pri | Доработка | Зачем | Сложность | Источник |
|-----|-----------|-------|-----------|----------|
| **P1** | ~~Заголовки подписки (`Subscription-Userinfo` и др.)~~ | Трафик / срок без панели провайдера | **S** | 3x-ui · **сделано 2026-08-13** |
| **P1** | ~~Clash / Mihomo YAML import~~ | Много провайдеров отдают только Clash | **M** | 3x-ui · **сделано 2026-08-15** |
| **P1** | ~~QR import (камера / картинка → URL / URI / текст)~~ | Удобный перенос с телефона | **S–M** | 3x-ui · **сделано 2026-08-15** (файл + буфер, без камеры) |
| **P1** | Autostart systemd + автоподключение личного VPN | Поле `autoConnect` есть, юнита нет | **S–M** | README |
| **P2** | SSRF-guard на fetch URL подписок | Безопасность | **S** | 3x-ui |
| **P2** | Route explain («куда уйдёт host») | Отладка правил без полного probe | **S–M** | 3x-ui |
| **P2** | Health-check личного VPN → reapply / reconnect | Меньше «залипших» сессий | **M** | 3x-ui |
| **P2** | Спидтест по путям (direct / work / personal) | Универсальный замер качества сети (Мбит/с, latency), не путать с probe «сайт открывается» | **M** | продукт |
| **P2** | Установщик `.deb` | Раздача без `make sync` из исходников | **M–L** | README |
| **P3** | Stats ↑↓ (sing-box / clash API) | Наглядность в UI | **M** | 3x-ui |
| **P3** | Backup / export настроек | Перенос на другую машину | **S–M** | 3x-ui |
| **P3** | In-app log viewer (`sing-box.log`) | Меньше `tail` в терминале | **S** | 3x-ui |
| **P3** | Импорт GeoSite / авто‑категории в правила | Каталоги доменов | **M** | README + 3x-ui |
| **P3** | Несколько активных подписок | Сейчас одна `activeSubscriptionId` | **M–L** | README |
| **P3** | Авто-failover leastPing по узлам | После стабильного ping | **M–L** | README + 3x-ui |
| later | JSON Xray sub, DNS presets UI, dialer chain, WG export | По боли | — | 3x-ui |

Сознательно **не** берём из 3x-ui: inbounds / REALITY scanner, квоты, Fail2ban, multi-node admin, серверный WARP, Telegram-бот админки.

---

## Рекомендуемый порядок внедрения

1. ~~**Userinfo headers** (P1, S)~~ — сделано  
2. ~~**Clash import** (P1, M)~~ — сделано  
3. ~~**QR** (P1, S–M)~~ — сделано (файл / буфер, без камеры)  
4. **Autostart systemd** (P1, S–M) — «само поднимается после логина»  
5. **SSRF-guard** (P2, S) — вместе с любым касанием `Fetch`  
6. **Route explain** (P2, S–M)  
7. **Health-check** (P2, M)  
8. **Спидтест по путям** (P2, M) — рядом с probe, но про throughput/latency  
9. **`.deb`** (P2, M–L) — когда стабилен sync/host  
10. P3 — по боли (stats / backup / logs / geosite / multi-sub / failover)

Каждый пункт: тесты на логику + `make check-fmt && make lint && make test` (или `make ci`).

---

## История

| Дата | Что |
|------|-----|
| 2026-08-13 | Сведён единый бэклог из README + `3x-ui-client-ideas.md` |
| 2026-08-13 | + P2 спидтест по путям (отдельно от probe сайта) |
| 2026-08-13 | P1 Subscription-Userinfo / Profile-* — сделано |
| 2026-08-13 | UX подписок: activate API, бейджи Выбрана/Не выбрана, автовыбор одного сервера, тип/даты на карточке |
| 2026-08-15 | P1 Clash / Mihomo YAML import — сделано |
| 2026-08-15 | P1 QR import (картинка / буфер, без камеры) — сделано |
