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
- `RemapSelection` при refresh (аналог их stable tags для outbound-subs);
- ping узлов, probe сайта по путям, coexist / recover.

## 2. Приоритеты (roadmap)

| Pri | Идея | Ценность | Оценка сложности |
|-----|------|----------|------------------|
| **P1** | Clash / Mihomo YAML import | Много провайдеров только так | M |
| **P1** | QR import (ссылка / текст / URL) | С телефона на десктоп | S–M |
| **P1** | Заголовки подписки (`Subscription-Userinfo` и др.) | Трафик/срок без панели провайдера | S |
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

### 3.1 Clash YAML import — P1

**Смысл:** вкладка/режим «Clash» рядом с URL/текст/файл; тело YAML → список узлов.

**Куда:**

- `internal/subscription/clash.go` — парсер proxies → `[]Node` (black-box тесты в `tests/subscription/`);
- расширить `addSubscription` / `ParseBody` (или отдельный `source=clash`);
- UI: `desktop/src/add-subscription.ts` + панель в `index.html` / `main.ts`.

**Подход:**

1. Парсить только `proxies:` (Mihomo/Clash Meta): `type: vless|vmess|trojan|ss|hysteria2|…`.
2. Для каждого прокси строить **канонический `rawUri`** (share-link), чтобы дальше жить на уже существующем `URIToOutbound` / sing-box пути — без второй ветки генерации конфига.
3. Неподдерживаемые типы пропускать с счётчиком `skipped` в ответе API (toast: «N узлов, M пропущено»).
4. Proxy-groups / rules из Clash **игнорировать** на первом круге (у нас своя модель direct/work/personal).

**Тесты:** фикстуры YAML (vless+reality, ss, hy2) в `tests/subscription/testdata/`.

**API:** additive (`source: "clash"`); при желании bump `apiVersion` только если ломаем старый клиент (не обязательно).

---

### 3.2 QR import — P1

**Смысл:** камера / картинка → строка → тот же пайплайн, что «Текст» / «Ссылка» / URL.

**Куда:** `desktop/` (Tauri): кнопка «Сканировать QR» на вкладке импорта; декод на клиенте; в API уходит уже `content` / `url`.

**Подход:**

1. Предпочтительно декод в UI (библиотека QR), без нового API.
2. После декода: если `http(s)://` → `source=url`; если `vless://…` → `source=uri`; иначе → `source=text`.
3. Linux: доступ к камере через Tauri permissions; fallback «выбрать изображение».

**Тесты:** unit на классификатор строки после декода (чистая функция рядом с `add-subscription.ts`).

---

### 3.3 Subscription HTTP headers — P1

**Смысл:** при `Fetch` URL сохранять метаданные провайдера.

Заголовки (как у 3x-ui / многих клиентов):

- `Subscription-Userinfo`: `upload=…; download=…; total=…; expire=…`
- `Profile-Title`, `Profile-Update-Interval`, `Announce`, `Support-Url`

**Куда:**

- `internal/subscription/fetch.go` — вернуть body + headers (или отдельный тип `FetchResult`);
- `internal/config.Subscription` — поля `Traffic*`, `ExpireAt`, `ProfileTitle`, `Announce` (omitempty);
- refresh / add URL — заполнять;
- UI карточка подписки — строка «осталось X GiB · до YYYY-MM-DD».

**Тесты:** парсер `Userinfo` в `tests/subscription/`; API refresh с `httptest` и кастомными headers.

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

1. **Userinfo headers** — быстрый win, почти без UI-ломки.  
2. **Clash import** — закрывает дыру форматов.  
3. **QR** — UX поверх уже расширенного импорта.  
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
