#!/usr/bin/env bash
set -euo pipefail
DEST="${HOME}/.local/bin/sing-box"
if [[ ! -x "$DEST" ]]; then
  echo "sing-box не найден: сначала make sync" >&2
  exit 1
fi
sudo setcap cap_net_admin,cap_net_bind_service=+ep "$DEST"
getcap "$DEST"
echo "Готово: sing-box может создавать TUN без root"
