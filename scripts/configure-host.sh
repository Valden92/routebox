#!/usr/bin/env bash
# Один раз sudo из терминала (или пароль в UI приложения).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${HOME}/.local/bin/sing-box"
export SING_BOX_DEST="$DEST"
sudo -v
sudo env SING_BOX_DEST="$DEST" bash "$ROOT/scripts/configure-host-inner.sh"
echo "Готово."
