#!/usr/bin/env bash
# Устарело: используйте make dev
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
exec make -C "$ROOT" dev
