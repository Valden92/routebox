# Go — стиль Router BOX

- Формат: `gofmt` / `goimports` (local prefix `github.com/Valden92/routebox`). Линт: `make lint-go`.
- Пакеты только в `internal/` (кроме `cmd/`). Linux-специфика — `*_linux.go`, заглушки — `*_stub.go`.
- Ошибки: оборачивать через `%w`; строки ошибок без заглавной буквы и точки в конце.
- Минимальный diff; не рефакторить соседний код «заодно».
- Перед правкой символа — GitNexus `impact` (см. [gitnexus.md](gitnexus.md)).
- Изменённая логика — unit-тест в `tests/` в той же задаче.

Область: `**/*.go`; для сетевых/API-контрактов дополнительно читать [project.md](project.md).
