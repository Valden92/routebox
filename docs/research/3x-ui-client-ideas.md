# Идеи из 3x-ui для Router BOX

**Источник:** [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) (веб-панель **серверного** Xray).  
**Дата разбора:** 2026-08-05.  
**Канвас в Cursor:** `canvases/3x-ui-feature-study.canvas.tsx` (обзорная таблица; канон — этот файл).

## 1. Контекст: разные продукты

| | 3x-ui | Router BOX |
|---|--------|------------|
| Роль | Админка VPS / Xray-core | Десктопный клиентский роутер (Linux) |
| Ядро | Xray | sing-box (`tun100`) + NM (`tun0` read-only) |
| Пользователь | Владелец сервера / раздача доступов | Один пользователь ноутбука |
| Подписки | **Отдаёт** `/sub` `/json` `/clash` | **Потребляет** URL / текст / URI / файл |

**Правило:** не форкать UI и не переносить инбаунды/квоты/Fail2ban/TG-бот. Брать клиентские форматы, UX-паттерны и идеи стабильности.

Уже сделано у нас (не копировать из 3x-ui):

- импорт URL + текст + share-URI + файл (`source=url|text|uri`);
- `RemapSelection` при refresh (аналог их stable tags для outbound-subs) + автовыбор единственного узла;
- ping узлов, probe сайта по путям, coexist / recover;
- заголовки подписки `Subscription-Userinfo` / `Profile-*` (квота и срок в UI);
- Clash / Mihomo YAML (`source=clash` + автодетект в `ParseBody`).

## 2. Приоритеты (roadmap)

**Канон приоритетов и сложностей:** [`docs/ROADMAP.md`](../ROADMAP.md).

Ниже — исходная таблица из разбора 3x-ui (для контекста §3); при расхождении побеждает `ROADMAP.md`.

| Pri | Идея | Ценность | Оценка сложности |
|-----|------|----------|------------------|
| **P1** | ~~Clash / Mihomo YAML import~~ | Много провайдеров только так | M · **сделано 2026-08-15** |
| **P1** | ~~QR import (ссылка / текст / URL)~~ | С телефона на десктоп | S–M · **сделано** (файл/буфер) |
| **P1** | ~~Заголовки подписки (`Subscription-Userinfo` и др.)~~ | Трафик/срок без панели провайдера | S · **сделано 2026-08-13** |
| **P2** | Route explain («куда уйдёт host») | Отладка правил рядом с probe | S–M |
| **P2** | Health-check личного VPN → reapply/reconnect | Меньше «залипших» сессий | M |
| **P2** | SSRF-guard на fetch URL | Безопасность | S |
| **P3** | Stats ↑↓ (sing-box API) | Наглядность | M |
| **P3** | Backup/export настроек | Онбординг / перенос машины | S–M |
| **P3** | In-app log viewer | Меньше `tail` в терминале | S |
| **P3** | Авто-failover leastPing | После стабильного ping | M–L |
| **P3** | Импорт geosite-наборов в правила | Каталоги доменов | M |
| later | JSON Xray sub, DNS presets UI, dialer chain, WG export | По боли | — |

Сознательно **не** берём: inbounds/REALITY scanner, квоты/expiry enforcement, Fail2ban, multi-node admin, WARP/Nord как серверный egress, admin Telegram-bot.

## 3. Как реализовывать у нас (по фичам)

### 3.1 Clash YAML import — P1 · **сделано 2026-08-15**

**Смысл:** вкладка/режим «Clash» рядом с URL/текст/файл; тело YAML → список узлов.

**Куда (реализовано):**

- `internal/subscription/clash.go` — `ParseClash` / `LooksLikeClash`;
- `source=clash` в `addSubscription`; автодетект в `ParseBody` и при `source=text`/`file`;
- UI: поле **Текст** (и файл `.yaml`) — без отдельной вкладки Clash.

**Подход:**

1. Парсить только `proxies:` (Mihomo/Clash Meta): `type: vless|ss|hysteria2`.
2. Для каждого прокси — канонический `rawUri` → существующий sing-box путь.
3. Неподдерживаемые типы → `skipped` в ответе API.
4. Proxy-groups / rules игнорируются.

**Тесты:** `tests/subscription/clash_test.go`, `testdata/clash-sample.yaml`, API `TestAddSubscriptionClash`.

---

### 3.2 QR import — P1 · **сделано 2026-08-15**

**Смысл:** картинка (файл / буфер) → строка → тот же пайплайн, что «Текст» / «Ссылка» / URL.

**Куда (реализовано):** `desktop/src/qr-import.ts` + вкладка «QR» в форме добавления; декод `jsQR` на клиенте; в API уходит уже `content` / `url` / `uri`. Камера **не** используется.

**Подход:**

1. Декод в UI (jsQR), без нового API.
2. После декода: если `http(s)://` → `source=url`; если одна share-ссылка → `source=uri`; иначе → `source=text`.
3. Источники картинки: выбор файла, кнопка «Вставить из буфера», Ctrl+V на вкладке QR.

**Тесты:** `desktop/src/qr-import.test.ts` (классификатор строки).

---

### 3.3 Subscription HTTP headers — P1 · **сделано 2026-08-13**

**Смысл:** при `Fetch` URL сохранять метаданные провайдера.

Заголовки (как у 3x-ui / многих клиентов):

- `Subscription-Userinfo`: `upload=…; download=…; total=…; expire=…`
- `Profile-Title`, `Profile-Update-Interval`, `Announce`, `Support-Url`

**Куда (реализовано):**

- `internal/subscription/fetch.go` → `FetchResult` + `meta.go` (`ParseUserinfo`, `DecodeHeaderText`);
- `internal/config.Subscription` — `Traffic*`, `ExpireAt`, `ProfileTitle`, `Announce`, …;
- add / refresh / scheduler — `applySubscriptionMeta`;
- UI: `formatSubscriptionQuota` на карточке.

**Тесты:** `tests/subscription/userinfo_test.go`; API refresh с headers в `tests/api/`.

---

### 3.4 Route explain — P2

**Смысл:** `GET/POST` «для host X какой путь?» без полного HTTP probe.

**Куда:**

- логика уже рядом с `internal/routing` (`SuggestPath` и фильтры правил);
- новый эндпоинт, например `POST /api/rules/explain` `{ "host": "…" }` → `{ path, matchedRuleId?, reason }`;
- UI: поле на вкладке правил или рядом с auto-check.

**Отличие от probe:** не ходит в сеть; только локальное решение по правилам + default.

**Тесты:** таблицы в `tests/routing/` + httptest в `tests/api/`.

---

### 3.5 Health-check личного VPN — P2

**Смысл:** как `internal/tunnelmonitor` в 3x-ui: периодический probe → N failures → действие.

**Куда:**

- воркер в демоне (рядом с refresh scheduler), не в UI;
- probe через personal path (уже есть зачатки в `internal/probe` / bind);
- действия по нарастающей: лог → toast через статус `personalVpn.error` / флаг → `reapply` → disconnect+connect (осторожно с coexist).

**Конфиг** (в settings или env): interval, URL, failures, cooldown — по аналогии с `XUI_TUNNEL_HEALTH_*`.

**Не делать в unit без stubs:** живой NM/sing-box Start — только integration позже.

---

### 3.6 SSRF-guard на fetch — P2

**Куда:** `subscription.Fetch` / хелпер `IsSafeRemoteURL` перед dial.

Блокировать: loopback, link-local, RFC1918 (опционально с allowlist для домашней лабы), metadata IPs.  
Тесты: таблица URL в `tests/subscription/`.

---

### 3.7 Stats / backup / logs — P3 (кратко)

| Фича | Реализация у нас |
|------|------------------|
| ↑↓ трафик | Включить clash/experimental API у sing-box в generated config; `GET /api/personal-vpn/stats`; карточка «Личный VPN» |
| Backup | `GET /api/backup` → zip/tar settings.json + subscriptions/*.json (+ без секретов lock?); UI «Экспорт/Импорт» |
| Log viewer | `GET /api/logs/sing-box?tail=N` (только `DataDir`); модалка в UI с фильтром `error` |

## 4. Порядок внедрения (рекомендуемый)

1. ~~**Userinfo headers**~~ — сделано.  
2. ~~**Clash import**~~ — сделано.  
3. ~~**QR**~~ — сделано (файл / буфер, без камеры).  
4. **SSRF-guard** — вместе с любым следующим касанием `Fetch`.  
5. **Route explain** — усиливает правила/probe.  
6. **Health-check** — стабильность.  
7. Остальное P3 — по боли.

Каждый пункт: тесты логики + `make ci` (см. `.cursor/rules/quality-gate.mdc`).

## 5. Ссылки на код 3x-ui (ориентиры, не копипаст)

| Тема | Где смотреть |
|------|----------------|
| Clash sub | `internal/sub/clash_service.go` |
| JSON sub | `internal/sub/json_service.go` |
| Sub headers / formats | `docs/content/docs/en/config/subscription.mdx` |
| Info page / `?format=info` | `docs/custom-subscription-templates.md` |
| Tunnel health | `internal/tunnelmonitor`, env `XUI_TUNNEL_HEALTH_*` |
| Outbound sub + latency test | `frontend/.../outbounds/SubscriptionOutbounds.tsx`, routing docs |
| Balancers / leastPing | `docs/.../outbounds-routing.mdx` |

## 6. История

| Дата | Что |
|------|-----|
| 2026-08-05 | Первый разбор + недооценённые паттерны; этот документ и канвас |
| 2026-08-13 | Приоритеты сведены в `docs/ROADMAP.md` (вместе с пунктами README) |
| 2026-08-13 | P1 Userinfo / Profile-* — сделано |
| 2026-08-15 | P1 Clash / Mihomo YAML import — сделано |
