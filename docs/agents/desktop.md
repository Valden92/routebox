# Desktop UI (TypeScript)

- Линт/формат: `make lint-ui` / `make fmt-ui` (ESLint + Prettier). Общие проверки — [quality.md](quality.md).
- API-клиент — `desktop/src/api.ts`; базовый URL `http://127.0.0.1:47891`.
- Не тащить React/Vue без явного решения; сейчас vanilla TS + Vite.
- Сохранять существующие паттерны карточек статуса и терминов: direct / системный VPN / личный VPN.
- **Логика → модуль + тест:** ветвления/маппинги выносить из `main.ts` (`ping-label`, `nodes-sort`, `ui-labels`, `status-ui`, `rules-util` и т.п.) и сразу покрывать Vitest (`*.test.ts`). Общий gate — [quality.md](quality.md).
- Запуск UI-тестов: `npm test` / `make test-ui`.

Область: `desktop/**/*.{ts,css,html}`, кроме Rust-обёртки.
