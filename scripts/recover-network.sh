#!/usr/bin/env bash
# Восстановление сети после личного VPN. Без sudo — иначе вылезает polkit.
set -euo pipefail
IFACE="${VPN_ROUTER_MAIN_IFACE:-wlp0s20f3}"

echo "Останавливаем sing-box…"
killall sing-box 2>/dev/null || pkill -x sing-box 2>/dev/null || true
sleep 1

# Залипшие ip rule/table sing-box (дефолт 2022/9000 и Router BOX 20221/9210).
# Без CAP_NET_ADMIN часть команд no-op — тогда: sudo ip rule / make sync после setcap.
for table in 20221 2022; do
  ip route flush table "$table" 2>/dev/null || true
done
while read -r line; do
  prio="${line%%:*}"
  case "$prio" in
    ''|*[!0-9]*) continue ;;
  esac
  if (( prio >= 9000 && prio < 9100 )) || (( prio >= 9210 && prio <= 9250 )); then
    ip rule del priority "$prio" 2>/dev/null || true
  fi
done < <(ip rule show 2>/dev/null || true)

GW="$(ip -4 route show default 2>/dev/null | awk -v d="$IFACE" '$0 ~ "dev " d {print $3; exit}')"
if [[ -z "$GW" ]]; then
  GW="$(ip -4 route show dev "$IFACE" 2>/dev/null | awk '/^default / {print $3; exit}')"
fi
if [[ -n "$GW" ]]; then
  ip route replace default via "$GW" dev "$IFACE" metric 100 2>/dev/null || true
  echo "Default route: via $GW dev $IFACE"
fi

echo "Готово. Проверка: ping -c1 1.1.1.1"
