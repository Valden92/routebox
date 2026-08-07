#!/usr/bin/env bash
# Один раз sudo из терминала (или пароль в UI приложения).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${HOME}/.local/bin/sing-box"
NETCLEAN="${HOME}/.local/bin/vpn-router-netclean"
mkdir -p "${HOME}/.local/bin"
# helper для очистки залипших ip rule (нужен setcap)
if command -v go >/dev/null 2>&1 || [[ -x "${HOME}/.local/go/bin/go" ]]; then
  export PATH="${HOME}/.local/go/bin:${HOME}/go/bin:${PATH}"
  (cd "$ROOT" && go build -o "$NETCLEAN" ./cmd/netclean/)
  chmod 755 "$NETCLEAN"
fi
export SING_BOX_DEST="$DEST"
export VPN_ROUTER_NETCLEAN_DEST="$NETCLEAN"
sudo -v
sudo env SING_BOX_DEST="$DEST" VPN_ROUTER_NETCLEAN_DEST="$NETCLEAN" bash "$ROOT/scripts/configure-host-inner.sh"
echo "Готово."
