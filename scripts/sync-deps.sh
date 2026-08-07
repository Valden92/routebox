#!/usr/bin/env bash
# Идемпотентная установка всех зависимостей проекта. Вызывается через: make sync
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="${HOME}/.local/go/bin:${HOME}/.cargo/bin:${HOME}/.local/bin:${PATH}"

GO_VERSION="${GO_VERSION:-1.24.2}"
GO_INSTALL_DIR="${HOME}/.local/go"
SING_BOX_VERSION="${SING_BOX_VERSION:-1.14.0-beta.9}"

APT_PACKAGES=(
  libwebkit2gtk-4.1-dev
  libjavascriptcoregtk-4.1-dev
  libsoup-3.0-dev
  build-essential
  pkg-config
  curl
  wget
  file
  libssl-dev
  librsvg2-dev
  libayatana-appindicator3-dev
  libxdo-dev
  libcap2-bin
)

PKG_CONFIG_CHECKS=(
  libsoup-3.0
  javascriptcoregtk-4.1
  webkit2gtk-4.1
)

log() { echo "==> $*"; }
ok()  { echo "    OK: $*"; }
skip() { echo "    skip: $*"; }
fail() { echo "ERROR: $*" >&2; }

# То, что пользователь/ОС должны дать до настройки Router BOX (sync сам это не ставит).
check_prerequisites() {
  local errors=0
  log "предусловия (до настройки Router BOX)"

  if [[ "$(uname -s)" != "Linux" ]]; then
    fail "нужен Linux (сейчас: $(uname -s)). macOS/Windows не поддерживаются."
    errors=1
  else
    ok "Linux"
  fi

  if [[ -r /etc/os-release ]]; then
    # shellcheck source=/dev/null
    . /etc/os-release
    ok "дистрибутив: ${PRETTY_NAME:-$ID}"
  fi

  if ! command -v nmcli >/dev/null 2>&1; then
    fail "не найден nmcli — установите NetworkManager (пакет network-manager)."
    errors=1
  else
    ok "nmcli"
    if systemctl is-active --quiet NetworkManager 2>/dev/null || \
       systemctl is-active --quiet NetworkManager.service 2>/dev/null; then
      ok "NetworkManager активен"
    elif pgrep -x NetworkManager >/dev/null 2>&1; then
      ok "NetworkManager запущен"
    else
      fail "NetworkManager не запущен. Включите: systemctl enable --now NetworkManager"
      errors=1
    fi
  fi

  if ! command -v resolvectl >/dev/null 2>&1; then
    fail "не найден resolvectl — нужен systemd-resolved (для DNS в режиме coexist)."
    errors=1
  else
    ok "resolvectl"
  fi

  if [[ ! -d /etc/polkit-1 ]]; then
    fail "не найден polkit (/etc/polkit-1) — установите polkit; без него личный VPN будет запрашивать пароль."
    errors=1
  else
    ok "polkit"
  fi

  if ! command -v ip >/dev/null 2>&1; then
    fail "не найден ip (iproute2)."
    errors=1
  else
    ok "iproute2"
  fi

  if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
    fail "нужен Node.js 20+ в PATH (https://nodejs.org или nvm). sync его не ставит."
    errors=1
  else
    local major
    major=$(node -p "process.versions.node.split('.')[0]" 2>/dev/null || echo 0)
    if [[ "$major" -lt 20 ]]; then
      fail "Node.js ${major} слишком старый — нужен 20+ (сейчас: $(node --version))."
      errors=1
    else
      ok "node $(node --version), npm $(npm --version)"
    fi
  fi

  if ! command -v sudo >/dev/null 2>&1; then
    fail "нужен sudo — первый sync ставит пакеты и настраивает setcap/polkit/NM."
    errors=1
  else
    if sudo -n true 2>/dev/null; then
      ok "sudo (без повторного пароля в этой сессии)"
    else
      ok "sudo доступен (при настройке хоста/apt может запросить пароль)"
    fi
  fi

  if [[ "$errors" -ne 0 ]]; then
    echo "" >&2
    fail "предусловия не выполнены — устраните ошибки выше и снова запустите: make sync"
    exit 1
  fi
}

have_pkg_config() {
  local name=$1
  pkg-config --exists "$name" 2>/dev/null
}

ensure_go() {
  if command -v go >/dev/null 2>&1; then
    ok "go $(go version | awk '{print $3}')"
    return
  fi
  if [[ -x "${GO_INSTALL_DIR}/bin/go" ]]; then
    ok "go $("${GO_INSTALL_DIR}/bin/go" version | awk '{print $3}')"
    return
  fi
  log "install Go ${GO_VERSION} -> ${GO_INSTALL_DIR}"
  local arch=linux-amd64
  local tgz="/tmp/go${GO_VERSION}.${arch}.tar.gz"
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.${arch}.tar.gz" -o "$tgz"
  rm -rf "${GO_INSTALL_DIR}"
  mkdir -p "${HOME}/.local"
  tar -C "${HOME}/.local" -xzf "$tgz"
  rm -f "$tgz"
  ok "go installed"
}

ensure_rust() {
  if command -v cargo >/dev/null 2>&1; then
    ok "cargo $(cargo --version)"
    return
  fi
  if [[ -f "${HOME}/.cargo/env" ]]; then
    # shellcheck source=/dev/null
    source "${HOME}/.cargo/env"
    if command -v cargo >/dev/null 2>&1; then
      ok "cargo $(cargo --version)"
      return
    fi
  fi
  log "install Rust (rustup)"
  curl -fsSL https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
  # shellcheck source=/dev/null
  source "${HOME}/.cargo/env"
  ok "rust $(rustc --version)"
}

ensure_node() {
  # Версия уже проверена в check_prerequisites; здесь только подтверждение для лога этапа.
  ok "node $(node --version), npm $(npm --version)"
}

ensure_system() {
  local missing=0
  for pc in "${PKG_CONFIG_CHECKS[@]}"; do
    if have_pkg_config "$pc"; then
      ok "pkg-config $pc"
    else
      echo "    need: $pc"
      missing=1
    fi
  done
  if [[ "$missing" -eq 0 ]]; then
    skip "apt packages (уже установлены)"
    return
  fi
  if ! command -v apt-get >/dev/null 2>&1; then
    echo "ERROR: не Debian/Ubuntu — установите вручную: ${APT_PACKAGES[*]}" >&2
    exit 1
  fi
  log "apt install (системные libs для Tauri)"
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y "${APT_PACKAGES[@]}"
  ok "system packages"
}

ensure_sing_box() {
  local dest="${HOME}/.local/bin/sing-box"
  if [[ -x "$dest" ]] && "$dest" version 2>/dev/null | grep -q "sing-box version ${SING_BOX_VERSION}"; then
    ok "sing-box ${SING_BOX_VERSION}"
    return
  fi
  log "sing-box ${SING_BOX_VERSION}"
  SING_BOX_VERSION="$SING_BOX_VERSION" "${ROOT}/scripts/install-sing-box.sh"
}

host_ready() {
  local dest="${HOME}/.local/bin/sing-box"
  local dropin="/etc/NetworkManager/conf.d/99-vpn-router.conf"
  local stamp="${HOME}/.config/vpn-router/host-ready.stamp"
  [[ -x "$dest" ]] || return 1
  command -v getcap >/dev/null 2>&1 || return 1
  getcap "$dest" 2>/dev/null | grep -q cap_net_admin || return 1
  [[ -f "$dropin" ]] || return 1
  grep -q 'interface-name:tun100' "$dropin" && grep -q unmanaged-devices "$dropin"
  ! grep -q 'type:tun' "$dropin" && ! grep -q 'interface-name:tun0' "$dropin"
  [[ -f "$stamp" ]] && grep -q 'nm+setcap+polkit-singbox+v7' "$stamp"
  local netclean="${HOME}/.local/bin/vpn-router-netclean"
  [[ -x "$netclean" ]] || return 1
  getcap "$netclean" 2>/dev/null | grep -q cap_net_admin || return 1
  [[ -f "${HOME}/.config/vpn-router/polkit-vpn-router.rules" ]]
}

ensure_host() {
  if host_ready; then
    skip "host (setcap + NetworkManager для TUN)"
    return
  fi
  log "настройка хоста для личного VPN (setcap + NetworkManager, один sudo)"
  chmod +x "${ROOT}/scripts/configure-host.sh" "${ROOT}/scripts/configure-host-inner.sh"
  chmod 644 "${ROOT}/scripts/polkit-vpn-router.rules" 2>/dev/null || true
  "${ROOT}/scripts/configure-host.sh"
  if ! host_ready; then
    echo "ERROR: настройка хоста не завершилась — проверьте: make configure-host" >&2
    exit 1
  fi
  ok "host configured"
}

sync_go_modules() {
  log "go mod download"
  (cd "$ROOT" && go mod download && go mod tidy)
  ok "go modules"
}

sync_npm() {
  log "npm install (desktop/)"
  (
    cd "$ROOT/desktop"
    if [[ -f package-lock.json ]]; then
      npm ci
    else
      npm install
    fi
  )
  ok "npm packages"
}

sync_cargo() {
  log "cargo fetch (Tauri)"
  (cd "$ROOT/desktop/src-tauri" && cargo fetch)
  ok "cargo crates"
}

sync_githooks() {
  if [[ -d "$ROOT/.git" ]] && [[ -f "$ROOT/.githooks/pre-commit" ]]; then
    log "git hooks"
    chmod +x "$ROOT/.githooks/pre-commit"
    git -C "$ROOT" config core.hooksPath .githooks
    ok "core.hooksPath=.githooks"
  else
    skip "git hooks (нет .git или .githooks)"
  fi
}

main() {
  log "sync: проверка окружения"
  check_prerequisites
  ensure_go
  ensure_rust
  ensure_node
  ensure_system
  ensure_sing_box
  ensure_host
  sync_go_modules
  sync_npm
  sync_cargo
  sync_githooks
  echo ""
  echo "sync: готово"
}

# Только предусловия (без установки пакетов / настройки хоста).
if [[ "${1:-}" == "--check-prereqs" ]]; then
  check_prerequisites
  echo "предусловия: OK"
  exit 0
fi

main "$@"
