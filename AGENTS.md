# Router BOX — руководство для агентов

Десктопное приложение для Linux: **Tauri UI** + **Go-демон** с трёхуровневой маршрутизацией трафика. Пользователь управляет личным VPN из приложения; системный (рабочий) VPN — только через NetworkManager/GNOME.

**Индекс GitNexus:** 594 символа, 48 execution flows (проиндексировано 2026-06-05). Репозиторий пока **без `.git`** — для переиндексации: `npx gitnexus analyze --skip-git`.

---

## Быстрый старт

```bash
make sync      # зависимости + setcap sing-box + polkit v6 + NM drop-in (sudo при первом разе)
make dev       # сборка демона + Tauri UI
make stop      # остановить демон и sing-box
```

- API: `http://127.0.0.1:47891`
- Конфиг пользователя: `~/.config/vpn-router/`
- Лог sing-box: `~/.config/vpn-router/sing-box.log`
- PID демона: `.daemon.pid` в корне проекта

Не коммитить без явной просьбы пользователя. Не добавлять широкие polkit-правила `set-default-route` для sudo — ломает MFA системного VPN.

---

## Архитектура

```
┌─────────────────┐     HTTP :47891    ┌──────────────────────────┐
│  desktop/       │ ─────────────────► │  cmd/daemon (Go)         │
│  Tauri + TS     │                    │  internal/api            │
└─────────────────┘                    └───────────┬──────────────┘
                                                   │
         ┌─────────────────────────────────────────┼─────────────────────────┐
         ▼                     ▼                   ▼                         ▼
   Wi-Fi (direct)      tun0 (system VPN)    tun100 (sing-box)        nmcli / resolvectl
   mainInterface       NM профиль PTsecurity  личный VPN               статус, DNS
```

| Компонент | Путь | Роль |
|-----------|------|------|
| Точка входа демона | `cmd/daemon/main.go` | Store, sing-box Manager, graceful shutdown + recover маршрутов |
| HTTP API | `internal/api/server.go`, `personal.go` | chi router, `apiVersion = 3` |
| sing-box | `internal/singbox/` | Генерация конфига, start/stop, coexist, recover |
| Системный VPN (read-only) | `internal/nm/workvpn.go` | Статус через NM, **без connect/disconnect** |
| Настройки | `internal/config/config.go` | JSON в `settings.json`, шифрование |
| Подписки | `internal/subscription/` | VLESS/Hysteria2, парсинг, fetch |
| UI | `desktop/src/main.ts`, `api.ts`, `index.html` | Карточки статуса, личный VPN |
| Хост | `scripts/configure-host-inner.sh`, `polkit-vpn-router.rules` | setcap, NM drop-in, polkit resolve1 только для `tun100` |

---

## Три пути маршрутизации

| `RoutePath` | UI / смысл | Интерфейс | Кто управляет |
|-------------|------------|-----------|---------------|
| `direct` | Прямой интернет | Wi-Fi (`mainInterface`) | — |
| `work` | «Системный VPN» | `tun0` (NM) | **Только ОС** (NetworkManager) |
| `personal` | Личный VPN | `tun100` (sing-box) | Приложение |

В API и JSON настроек поле `systemVpn` (миграция со старого `workVpn`). Эндпоинтов `POST /api/work-vpn/*` **нет**.

---

## Режим coexist (критично)

Когда системный VPN активен **и** включён личный, sing-box пишет конфиг в режиме **`coexist`**:

- `route_address`: `/1` (изоляция default route)
- `exclude_interface`: `tun0`
- outbound `work` для корпоративных маршрутов
- DNS `dns-work` с корпоративным резолвером
- **`hijack-dns` включён** (без него личные сайты не работают)

Ключевые файлы: `internal/singbox/coexist_linux.go`, `manager.go` (`WriteConfig`), `config_validate.go`.

Статус API отдаёт `configMode`, `configFileMode`, `configStale`. Если `configStale: true` — на диске старый `sing-box.json`; нужен reconnect или `POST /api/personal-vpn/reapply`.

**Порядок для пользователя:** сначала системный VPN в GNOME → потом выкл/вкл личный в приложении.

Проверка coexist на машине:

```bash
grep -E 'exclude_interface|route_address|"tag": "work"|hijack-dns' ~/.config/vpn-router/sing-box.json
ip link show tun100
```

---

## API демона (кратко)

| Метод | Путь | Назначение |
|-------|------|------------|
| GET | `/api/health`, `/api/version`, `/api/status` | Живость, фичи, сводный статус |
| GET/PUT | `/api/settings` | Настройки |
| POST | `/api/lock`, `/api/unlock` | Шифрование конфига |
| GET | `/api/personal-vpn/readiness` | Готовность хоста (setcap, NM) |
| POST | `/api/personal-vpn/connect` | stop → write config → start |
| POST | `/api/personal-vpn/disconnect` | stop + recover маршрутов |
| POST | `/api/personal-vpn/reapply` | Перезапись конфига без смены узла |
| GET | `/api/personal-vpn/status` | Детальный статус |
| * | `/api/subscriptions/*` | CRUD, refresh, ping, select node |
| * | `/api/rules/*` | Правила доменов/приложений |
| GET | `/api/apps` | Скан процессов (Linux) |
| POST | `/api/probe/site` | Проверка URL по путям |

UI ходит в API из `desktop/src/api.ts` (`const API = "http://127.0.0.1:47891"`).

---

## Настройка хоста и polkit

`make sync` вызывает `scripts/sync-deps.sh` → `configure-host.sh` → `configure-host-inner.sh`:

1. `setcap cap_net_admin,cap_net_bind_service+ep` на `~/.local/bin/sing-box`
2. NM drop-in: `interface-name:tun100` (не трогать `tun0`)
3. Polkit v6: `resolve1` разрешён **только для `tun100`**; для `tun0` — `NO`

Файл правил: `scripts/polkit-vpn-router.rules` (копия в `~/.config/vpn-router/`).

`HostReady()` в `internal/singbox/host_linux.go` — gate перед connect. Без sync личный VPN недоступен, UI показывает баннер.

---

## Recover сети

При аварийном падении sing-box маршруты могут «залипнуть»:

```bash
make recover-network   # scripts/recover-network.sh
make stop              # тоже гасит sing-box
```

Логика восстановления default `tun0`: `internal/singbox/recover.go`, вызывается при shutdown демона и disconnect.

---

## Типичные симптомы

| Симптом | Вероятная причина | Действие агента |
|---------|-------------------|-----------------|
| Личные сайты не работают при системном VPN | Старый `sing-box.json` без coexist | Reapply/connect; проверить grep выше |
| `127.0.0.1:53 connection refused` в логе | Битый DNS-конфиг | Пересобрать конфиг через connect/reapply |
| Окна пароля при личном VPN | Старый polkit | `make sync` (v6) |
| MFA не приходит вне приложения | Широкое polkit `set-default-route` | Удалить; не добавлять обратно |
| UI «включён», статус «Выключен» | Демон/sing-box упал | `make stop && make dev`, смотреть `sing-box.log` |

---

## Соглашения при правках

1. **Минимальный diff** — не трогать несвязанный код.
2. **Go:** пакеты в `internal/`, build tags `_linux.go` / `_stub.go` для платформ.
3. **Системный VPN:** только чтение статуса; не возвращать connect/disconnect API.
4. **Coexist:** любое изменение `WriteConfig` — проверить `coexist_linux_test.go` и `config_validate.go`.
5. **Polkit/NM:** изменения в `scripts/` требуют bump stamp в `configure-host-inner.sh`.
6. **API version:** при ломающих изменениях API — увеличить `apiVersion` в `server.go` и типы в `desktop/src/api.ts`.
7. Перед рефакторингом — GitNexus `impact` / `context` (см. ниже).

### Где искать по задаче

| Задача | Файлы |
|--------|-------|
| Личный VPN connect/disconnect | `internal/api/personal.go`, `internal/singbox/manager.go` |
| Coexist + маршруты | `internal/singbox/coexist_linux.go`, `manager.go`, `routes_linux.go` |
| Статус системного VPN | `internal/nm/workvpn.go`, `server.go` → `status()` |
| UI карточки | `desktop/src/main.ts`, `desktop/index.html` |
| Подписки/узлы | `internal/subscription/`, `internal/api/server.go` |
| Пинг без work VPN | `internal/ping/`, `internal/network/bind_linux.go` |

---

## GitNexus — навигация по коду

Проект проиндексирован как **vpn-router**. MCP-сервер `user-gitnexus` доступен в Cursor.

> Если инструменты предупреждают о stale index: `npx gitnexus analyze --skip-git` (пока нет git) или `npx gitnexus analyze` после `git init`.

### Кластеры (функциональные области)

| Кластер | Символов | Фокус |
|---------|----------|-------|
| Singbox | 42 | Конфиг, TUN, coexist, recover |
| Nm | 23 | NetworkManager, системный VPN |
| Api | 11 | HTTP handlers |
| Config | 8 | settings.json, шифрование |
| Subscription | 9 | Подписки, парсинг |
| Network | 6 | Интерфейсы, bind, HTTP client |

### Когда использовать

| Ситуация | Инструмент |
|----------|------------|
| «Как устроено X?» | `gitnexus_query({query: "..."})` |
| Контекст символа | `gitnexus_context({name: "WriteConfig"})` |
| Перед правкой функции | `gitnexus_impact({target: "X", direction: "upstream"})` |
| Перед коммитом | `gitnexus_detect_changes({scope: "staged"})` |
| Переименование | `gitnexus_rename({symbol_name, new_name, dry_run: true})` |

### Ресурсы

- `gitnexus://repo/vpn-router/context` — обзор и свежесть индекса
- `gitnexus://repo/vpn-router/clusters` — модули
- `gitnexus://repo/vpn-router/processes` — execution flows
- `gitnexus://repo/vpn-router/process/{name}` — пошаговый трейс

### Impact risk

| Depth | Значение |
|-------|----------|
| d=1 | Прямые вызывающие — **обязательно обновить** |
| d=2 | Косвенные зависимости — протестировать |
| d=3 | Транзитивные — по критичности пути |

### Skills (в репозитории)

| Задача | Файл |
|--------|------|
| Архитектура | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Отладка | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Рефакторинг | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |

### Обновление индекса

```bash
npx gitnexus analyze --skip-git   # без git-репозитория
npx gitnexus analyze              # после git init
npx gitnexus analyze --embeddings  # сохранить embeddings (если были)
```

Проверка: `.gitnexus/meta.json` → `stats.nodes`, `indexedAt`.

---

## Чеклист агента перед завершением задачи

1. Изменения соответствуют трём путям маршрутизации и read-only системному VPN.
2. Coexist/polkit не сломаны (если трогали `singbox/` или `scripts/`).
3. `make build` проходит (при изменениях Go).
4. Для нетривиальных правок — `gitnexus_impact` без игнорирования HIGH/CRITICAL.
5. Пользователю указаны команды проверки (`make dev`, grep конфига, лог).
