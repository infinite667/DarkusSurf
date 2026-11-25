#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
command -v go >/dev/null || { echo 'Go 1.23+ required: https://go.dev/dl/'; exit 1; }
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/DarkusSurf .
echo 'Built dist/DarkusSurf; requires tor and xdg-open on Linux, or a verified Linux bundle adjacent to the binary.'
