#!/usr/bin/env bash
# Запускается от root. Снимает настройку хоста Router BOX (обратное к configure-host-inner).
set -euo pipefail
DEST="${SING_BOX_DEST:-}"
NM_DROPIN="/etc/NetworkManager/conf.d/99-vpn-router.conf"
POLKIT_RULE="/etc/polkit-1/rules.d/50-vpn-router.rules"

if [[ -f "$NM_DROPIN" ]]; then
  rm -f "$NM_DROPIN"
  echo "removed $NM_DROPIN"
  systemctl reload NetworkManager 2>/dev/null || service network-manager reload 2>/dev/null || true
else
  echo "no NM drop-in"
fi

if [[ -f "$POLKIT_RULE" ]]; then
  rm -f "$POLKIT_RULE"
  echo "removed $POLKIT_RULE"
  systemctl reload polkit 2>/dev/null || true
else
  echo "no polkit rule"
fi

if [[ -n "$DEST" && -e "$DEST" ]]; then
  setcap -r "$DEST" 2>/dev/null || true
  echo "cleared capabilities on $DEST"
  getcap "$DEST" 2>/dev/null || echo "(no capabilities)"
fi

if [[ -n "${SUDO_USER:-}" ]]; then
  uhome=$(getent passwd "$SUDO_USER" | cut -d: -f6)
  if [[ -n "$uhome" ]]; then
    stamp_dir="$uhome/.config/vpn-router"
    rm -f "$stamp_dir/host-ready.stamp" "$stamp_dir/polkit-vpn-router.rules"
    echo "removed host stamps in $stamp_dir"
  fi
fi

echo "host reset done"
