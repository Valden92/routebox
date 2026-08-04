#!/usr/bin/env bash
# Запустите в одном терминале, затем включите личный VPN в приложении.
set -euo pipefail
echo "Слушаем polkit (15 с)… Включите личный VPN сейчас."
timeout 15 dbus-monitor --system "interface='org.freedesktop.PolicyKit1',member='CheckAuthorization'" 2>/dev/null | tee /tmp/vpn-router-polkit.log || true
echo "--- последние строки ---"
tail -20 /tmp/vpn-router-polkit.log 2>/dev/null || echo "(пусто — окна могут быть не polkit, а NetworkManager VPN)"
