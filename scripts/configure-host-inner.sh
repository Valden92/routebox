#!/usr/bin/env bash
# Запускается от root (через sudo -S). Без вложенного sudo.
set -euo pipefail
DEST="${SING_BOX_DEST:?SING_BOX_DEST required}"
NM_DROPIN="/etc/NetworkManager/conf.d/99-vpn-router.conf"
NETCLEAN_DEST="${VPN_ROUTER_NETCLEAN_DEST:-}"

if [[ ! -x "$DEST" ]]; then
  echo "sing-box не найден: $DEST" >&2
  exit 1
fi

mkdir -p /etc/NetworkManager/conf.d
tee "$NM_DROPIN" >/dev/null <<'EOF'
# Router BOX: только tun100 (личный sing-box). tun0 = рабочий VPN — должен оставаться managed (MFA).
[keyfile]
unmanaged-devices=interface-name:tun100
EOF
systemctl reload NetworkManager 2>/dev/null || service network-manager reload 2>/dev/null || true
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p /etc/polkit-1/rules.d
install -m 644 "$SCRIPT_DIR/polkit-vpn-router.rules" /etc/polkit-1/rules.d/50-vpn-router.rules
systemctl reload polkit 2>/dev/null || true
setcap cap_net_admin,cap_net_bind_service=+ep "$DEST"
getcap "$DEST"
if [[ -n "$NETCLEAN_DEST" && -x "$NETCLEAN_DEST" ]]; then
  setcap cap_net_admin=+ep "$NETCLEAN_DEST"
  getcap "$NETCLEAN_DEST"
fi
# rules.d (root:polkitd 750) обычный пользователь не видит — маркер для make sync
if [[ -n "${SUDO_USER:-}" ]]; then
  uhome=$(getent passwd "$SUDO_USER" | cut -d: -f6)
  if [[ -n "$uhome" && -d "$uhome" ]]; then
    stamp_dir="$uhome/.config/vpn-router"
    mkdir -p "$stamp_dir"
    echo "nm+setcap+polkit-singbox+v7" >"$stamp_dir/host-ready.stamp"
    cp -f "$SCRIPT_DIR/polkit-vpn-router.rules" "$stamp_dir/polkit-vpn-router.rules"
    chmod 644 "$stamp_dir/polkit-vpn-router.rules"
    chown -R "$SUDO_USER:$(id -gn "$SUDO_USER")" "$stamp_dir"
  fi
fi
