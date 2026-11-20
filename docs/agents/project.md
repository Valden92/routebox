# Архитектура и контракты Router BOX

Область: предметная модель, сетевые ограничения, sing-box/NM/polkit и совместимость API.

## Архитектура и критичные ограничения

- Tauri + vanilla TypeScript/Vite обращается к Go-демону на `127.0.0.1:47891`.
  Демон управляет sing-box; Tauri также может запускать/останавливать свой дочерний
  демон. Запуск второго UI для проверки не считать безопасным по умолчанию.
- Три пути: `direct` — основной интерфейс; `work` — системный VPN NetworkManager
  (обычно профиль `PTsecurity`, `tun0`); `personal` — личный VPN через sing-box.
  Системный VPN подключает и отключает только ОС. API использует `systemVpn`,
  мигрирует старое `workVpn`; не возвращать `POST /api/work-vpn/*`.
- `tun100` — постоянно работающий маршрутизатор. Выключение личного VPN отключает
  его выход `proxy`, но оставляет маршрутизацию. `running` личного VPN и
  `routingRunning` — разные состояния. `ensureRouter` перезапускает sing-box;
  connect/disconnect/reapply, смена узла и правки правил могут вызывать его.
- Coexist при активном системном VPN: `route_address` из двух IPv4 `/1`,
  `exclude_interface` для системного TUN, outbound `work`, корпоративный DNS
  `dns-work` и обязательный `hijack-dns`. Не перехватывать системный VPN.
  Режим меняется и фоновым watcher; `configMode`/`configFileMode`/`configStale`
  показывают соответствие режима на диске. Stale сам по себе не разрешает reapply.
- Пользовательские данные: `~/.config/vpn-router/`; основные файлы —
  `settings.json`, `sing-box.json`, `sing-box.log`, `subscriptions/`.
  PID демона — `.daemon.pid` в checkout. Не редактировать их ради тестов.
- `HostReady()` — `internal/singbox/host_setup.go`; проверка NM — `host_linux.go`.
  NM должен игнорировать только `tun100`, сохраняя управление системным TUN/MFA.
  Не добавлять широкие polkit-разрешения `set-default-route`.
- Recover и `vpn-router-netclean` изменяют таблицы/правила маршрутизации хоста.
  Не запускать их для профилактики, проверки или завершения пробного экземпляра.

Команды ручного запуска и типовые симптомы описаны в `README.md`.
Наличие там `make dev`, `make stop`, `make sync` и `make recover-network`
не отменяет обязательного согласования воздействия на действующее соединение.

---

## Контракты изменений

- **Системный VPN:** только чтение статуса; не возвращать connect/disconnect API.
- **Coexist:** любое изменение `WriteConfig` — проверить `coexist_linux_test.go` и `config_validate.go`.
- **OpenVPN:** только inline certs в `.ovpn`; endpoint `openvpn-client` с `tag: proxy`, `system: false`, `route_no_pull: true`, `mtu: 1400`. Нужен sing-box **1.14+** (beta pin в `SING_BOX_VERSION`). TUN: `dns_mode: disabled`, `stack: system`, `mtu: 1400`, на Linux `auto_redirect: true` и `strict_route: false` (иначе SO_BINDTODEVICE/`bind_interface` зацикливается).
- **Polkit/NM:** изменения в `scripts/` требуют bump stamp в `configure-host-inner.sh` (сейчас **v7**: + `vpn-router-netclean` setcap).
- **API version:** при ломающих изменениях API — увеличить `apiVersion` в `server.go` и типы в `desktop/src/api.ts`.

API v7: производственный HTTP-обработчик в `cmd/daemon/main.go` обязательно
обёрнут в `LocalAccess.Handler` (loopback, Origin/Host, Bearer-токен). Не публиковать
роутер в обход этой границы. `NewTestServer` создаёт обработчики без фоновых задач
для компонентных тестов; тесты защиты отдельно оборачивают его тем же `LocalAccess`.
Секрет читается нативной командой Tauri из приватного файла, не выдаётся по HTTP.

### Где искать по задаче

| Задача | Файлы |
|--------|-------|
| Личный VPN connect/disconnect | `internal/api/personal.go`, `internal/singbox/manager.go` |
| Coexist + маршруты | `internal/singbox/coexist_linux.go`, `manager.go`, `routes_linux.go` |
| Статус системного VPN | `internal/nm/workvpn.go`, `server.go` → `status()` |
| UI карточки | `desktop/src/main.ts`, `desktop/index.html` |
| Подписки/узлы | `internal/subscription/`, `internal/api/server.go` |
| Автовыбор узла по сайтам | `internal/api/autoselect.go`, `internal/singbox/probeconfig.go`, `desktop/src/autoselect.ts` |
| Пинг без work VPN | `internal/ping/`, `internal/network/bind_linux.go` |
| Идеи фич / бэклог | `docs/ROADMAP.md` (приоритеты); детали 3x-ui — `docs/research/3x-ui-client-ideas.md` |
