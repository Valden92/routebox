#!/usr/bin/env bash
set -euo pipefail
VER="${SING_BOX_VERSION:-1.14.0-beta.9}"
ARCH="linux-amd64"
DEST="${HOME}/.local/bin/sing-box"
URL="https://github.com/SagerNet/sing-box/releases/download/v${VER}/sing-box-${VER}-${ARCH}.tar.gz"
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
curl -fsSL "$URL" | tar -xz -C "$tmpdir"
install -m 755 "$tmpdir/sing-box-${VER}-${ARCH}/sing-box" "$DEST"
echo "Installed sing-box $VER -> $DEST"
"$DEST" version
echo "Права TUN и NetworkManager: make sync (или make configure-host)"
