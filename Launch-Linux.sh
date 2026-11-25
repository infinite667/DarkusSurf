#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
[[ -x dist/DarkusSurf ]] || ./scripts/Build-Linux.sh
exec ./dist/DarkusSurf
