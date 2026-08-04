#!/usr/bin/env bash
# Восстановление сети после личного VPN. Без sudo — иначе вылезает polkit.
set -euo pipefail
IFACE="${VPN_ROUTER_MAIN_IFACE:-wlp0s20f3}"

echo "Останавливаем sing-box…"
killall sing-box 2>/dev/null || pkill -x sing-box 2>/dev/null || true
sleep 1

GW="$(ip -4 route show default 2>/dev/null | awk -v d="$IFACE" '$0 ~ "dev " d {print $3; exit}')"
if [[ -z "$GW" ]]; then
  GW="$(ip -4 route show dev "$IFACE" 2>/dev/null | awk '/^default / {print $3; exit}')"
fi
if [[ -n "$GW" ]]; then
  ip route replace default via "$GW" dev "$IFACE" metric 100 2>/dev/null || true
  echo "Default route: via $GW dev $IFACE"
fi

echo "Готово. Проверка: ping -c1 1.1.1.1"
