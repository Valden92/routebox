# Router BOX — make sync | make dev | make help

SHELL        := /bin/bash
ROOT         := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
GO           ?= go
GOPATH_BIN   := $(shell $(GO) env GOPATH 2>/dev/null)/bin
export PATH  := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(GOPATH_BIN):$(HOME)/.cargo/bin:$(HOME)/.local/bin:$(PATH)

CARGO_TARGET ?= x86_64-unknown-linux-gnu
DAEMON       := $(ROOT)/bin/vpn-router-daemon
SIDECAR      := $(ROOT)/desktop/src-tauri/binaries/vpn-router-daemon-$(CARGO_TARGET)
API_ADDR     ?= 127.0.0.1:47891
PID_FILE     := $(ROOT)/.daemon.pid
SYNC_SCRIPT  := $(ROOT)/scripts/sync-deps.sh

.PHONY: all dev run build sidecar daemon daemon-fg desktop sync stop restart clean help \
	install-sing-box setcap-sing-box configure-host reset-host reset-host-wipe recover-network \
	deps deps-apt sync-go sync-node sync-rust check-prereqs \
	lint lint-go lint-ui lint-rust fmt fmt-go fmt-ui fmt-rust check-fmt test lint-tools hooks precommit ci

.DEFAULT_GOAL := dev

all: dev

.PHONY: build sidecar

build:
	@mkdir -p $(ROOT)/bin
	$(GO) build -o $(DAEMON) $(ROOT)/cmd/daemon/
	@$(MAKE) sidecar

sidecar:
	@mkdir -p $(dir $(SIDECAR))
	cp -f $(DAEMON) $(SIDECAR)
	@echo "built $(DAEMON)"

## Все зависимости: apt, sing-box, go/rust toolchain, go mod, npm, cargo
sync:
	@chmod +x $(SYNC_SCRIPT) $(ROOT)/scripts/install-sing-box.sh
	@$(SYNC_SCRIPT)

## Только предусловия ОС/пользователя (без установки)
check-prereqs:
	@chmod +x $(SYNC_SCRIPT)
	@$(SYNC_SCRIPT) --check-prereqs

# Алиасы (всё сводится к sync)
deps sync-go sync-node sync-rust deps-apt install-sing-box: sync

setcap-sing-box configure-host:
	@chmod +x $(ROOT)/scripts/configure-host.sh $(ROOT)/scripts/setcap-sing-box.sh
	@$(ROOT)/scripts/configure-host.sh

## Сбросить хост-настройку (polkit/NM/setcap); пользовательский конфиг сохранить
reset-host:
	@chmod +x $(ROOT)/scripts/reset-host.sh $(ROOT)/scripts/reset-host-inner.sh
	@$(ROOT)/scripts/reset-host.sh

## То же + бэкап и удаление ~/.config/vpn-router
reset-host-wipe:
	@chmod +x $(ROOT)/scripts/reset-host.sh $(ROOT)/scripts/reset-host-inner.sh
	@RESET_WIPE_CONFIG=1 $(ROOT)/scripts/reset-host.sh

recover-network:
	@chmod +x $(ROOT)/scripts/recover-network.sh
	@$(ROOT)/scripts/recover-network.sh

daemon: build
	-$(MAKE) stop
	@sleep 0.2
	@VPN_ROUTER_ROOT="$(ROOT)" "$(DAEMON)" --listen $(API_ADDR) & \
	echo $$! > "$(PID_FILE)"; \
	sleep 0.3; \
	echo "daemon started on http://$(API_ADDR) (pid $$(cat "$(PID_FILE)"))"

daemon-fg: build
	"$(DAEMON)" --listen $(API_ADDR)

stop:
	@if [ -f "$(PID_FILE)" ]; then \
		pid=$$(cat "$(PID_FILE)" 2>/dev/null); \
		if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
			kill "$$pid" 2>/dev/null && echo "stopping pid $$pid" || true; \
			for i in {1..50}; do \
				kill -0 "$$pid" 2>/dev/null || break; \
				sleep 0.1; \
			done; \
		fi; \
		rm -f "$(PID_FILE)"; \
	fi
	@(killall sing-box 2>/dev/null || pkill -x sing-box 2>/dev/null) && echo "stopped sing-box" || true
	@(killall vpn-router-daemon 2>/dev/null || true) && echo "stopped vpn-router-daemon" || true

restart: stop daemon

desktop: build
	cd $(ROOT)/desktop && npm run tauri dev

dev run: build daemon desktop

dev-full: sync build daemon desktop

clean:
	rm -rf $(ROOT)/bin $(ROOT)/desktop/dist $(ROOT)/desktop/node_modules
	rm -f $(PID_FILE) $(SIDECAR)

## Инструменты линтинга (golangci-lint в ~/.local/go/bin)
lint-tools:
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	@cd $(ROOT)/desktop && npm install

lint: lint-go lint-ui

test: test-go test-ui

test-go:
	cd $(ROOT) && $(GO) test ./tests/... ./internal/...

test-ui:
	cd $(ROOT)/desktop && npm test

lint-go:
	@command -v golangci-lint >/dev/null || { echo "run: make lint-tools"; exit 1; }
	cd $(ROOT) && golangci-lint run ./cmd/... ./internal/... ./tests/...

lint-ui:
	cd $(ROOT)/desktop && npm run lint

# Нужен stub/реальный sidecar (tauri-build проверяет externalBin).
lint-rust:
	@mkdir -p $(dir $(SIDECAR))
	@test -e "$(SIDECAR)" || touch "$(SIDECAR)"
	cd $(ROOT)/desktop/src-tauri && cargo clippy -- -W clippy::all

## Только проверка формата (без правок) — для CI
check-fmt:
	@bad=$$(cd $(ROOT) && gofmt -l cmd internal tests); \
	if [ -n "$$bad" ]; then echo "gofmt needed:"; echo "$$bad"; exit 1; fi
	cd $(ROOT)/desktop && npm run format:check
	cd $(ROOT)/desktop/src-tauri && cargo fmt --check

fmt: fmt-go fmt-ui fmt-rust

fmt-go:
	cd $(ROOT) && $(GO) fmt ./cmd/... ./internal/... ./tests/...
	@command -v goimports >/dev/null && cd $(ROOT) && goimports -w -local github.com/Valden92/routebox ./cmd ./internal ./tests || true

fmt-ui:
	cd $(ROOT)/desktop && npm run fmt

fmt-rust:
	cd $(ROOT)/desktop/src-tauri && cargo fmt

## Git hooks: core.hooksPath → .githooks (pre-commit: check-fmt + lint + test)
hooks:
	@chmod +x $(ROOT)/.githooks/pre-commit
	@git -C $(ROOT) config core.hooksPath .githooks
	@echo "git hooks: core.hooksPath=.githooks (pre-commit → make check-fmt lint test)"

## То же, что pre-commit hook / CI Format+Lint+Test (гонять вручную и агентам)
precommit: check-fmt lint test
ci: precommit

help:
	@echo "Router BOX"
	@echo ""
	@echo "  make sync     — зависимости + setcap/NM для личного VPN (sudo при первом разе)"
	@echo "  make check-prereqs — проверить Linux/NM/polkit/Node до sync"
	@echo "  make hooks    — включить git pre-commit (формат + линт + тесты)"
	@echo "  make precommit — check-fmt + lint + test (как хук / CI)"
	@echo "  make ci       — алиас make precommit"
	@echo "  make dev      — сборка + демон + Tauri (личный VPN без пароля в UI)"
	@echo "  make dev-full — sync + dev (рекомендуется новому пользователю)"
	@echo "  make daemon   — только API http://$(API_ADDR)"
	@echo "  make desktop  — только UI"
	@echo "  make stop     — остановить демон"
	@echo "  make build    — собрать демон"
	@echo "  make lint     — Go (golangci) + ESLint"
	@echo "  make lint-rust — clippy (Tauri; тяжёлый, в CI отдельно по paths)"
	@echo "  make check-fmt — проверка формата (gofmt / Prettier / rustfmt)"
	@echo "  make fmt      — автоформат Go / Prettier+ESLint / rustfmt"
	@echo "  make test     — Go tests + Vitest (desktop)"
	@echo "  make test-go / test-ui — по отдельности"
	@echo "  make lint-tools — поставить golangci-lint и npm lint deps"
	@echo "  make configure-host — повторить настройку TUN/NM (обычно не нужно после sync)"
	@echo "  make reset-host — сбросить polkit/NM/setcap (конфиг сохранить); затем make sync"
	@echo "  make reset-host-wipe — reset-host + бэкап и удаление ~/.config/vpn-router"
	@echo "  make recover-network — если пропал интернет после личного VPN"
	@echo "  make clean"
