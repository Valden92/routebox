#!/usr/bin/env bash
# Снимает системную настройку хоста Router BOX (polkit, NetworkManager drop-in, setcap).
# Пользовательский конфиг (~/.config/vpn-router) по умолчанию не трогается.
#
# Полный снос конфига (с бэкапом):
#   RESET_WIPE_CONFIG=1 ./scripts/reset-host.sh
#   # или: make reset-host-wipe
#
# После сброса: make sync && make dev
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${HOME}/.local/bin/sing-box"
CFG="${HOME}/.config/vpn-router"
WIPE_CONFIG="${RESET_WIPE_CONFIG:-0}"

echo "==> остановка демона / sing-box"
make -C "$ROOT" stop 2>/dev/null || true
pkill -f 'vpn-router-daemon' 2>/dev/null || true
pkill -f '[s]ing-box.*vpn-router' 2>/dev/null || true
sleep 0.3
if [[ -x "$ROOT/scripts/recover-network.sh" ]]; then
  bash "$ROOT/scripts/recover-network.sh" 2>/dev/null || true
fi

echo "==> сброс хоста (sudo: NM drop-in, polkit, setcap)"
chmod +x "$ROOT/scripts/reset-host-inner.sh"
sudo -v
sudo env SING_BOX_DEST="$DEST" bash "$ROOT/scripts/reset-host-inner.sh"

rm -f "$CFG/host-ready.stamp" "$CFG/polkit-vpn-router.rules" 2>/dev/null || true

if [[ "$WIPE_CONFIG" == "1" ]]; then
  if [[ -d "$CFG" ]]; then
    bak="${CFG}.bak.$(date +%Y%m%d-%H%M%S)"
    echo "==> бэкап конфига → $bak"
    mv "$CFG" "$bak"
    echo "каталог $CFG удалён (бэкап: $bak)"
  else
    echo "каталог конфига отсутствует: $CFG"
  fi
else
  echo "==> пользовательский конфиг сохранён: $CFG"
  echo "    полный снос конфига: make reset-host-wipe"
fi

echo
echo "Сброс хоста завершён."
echo "Далее:"
echo "  make sync    # setcap + polkit + NetworkManager (может запросить sudo)"
echo "  make dev"
echo
echo "Пока make sync не выполнен, личный VPN в UI будет недоступен"
echo "или система может запрашивать аутентификацию при управлении DNS/маршрутами."
