#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mkdir -p release deploy/bin cmd/installer/payload

gofmt -w cmd internal
go vet ./...
go test -race ./...

if command -v node >/dev/null 2>&1; then
  node --check internal/pasadatos/web/assets/desktop.js
  node --check internal/pasadatos/web/assets/mobile.js
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" \
  -o deploy/bin/pasadatos-server-linux-amd64 ./cmd/pasadatos

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w -H=windowsgui" \
  -o release/pasaDATOS-Windows-x64.exe ./cmd/pasadatos

cp release/pasaDATOS-Windows-x64.exe cmd/installer/payload/pasaDATOS.exe
cp internal/pasadatos/web/assets/pasaDATOS.ico cmd/installer/payload/pasaDATOS.ico

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w -H=windowsgui" \
  -o release/pasaDATOS-Setup-Windows-x64.exe ./cmd/installer

(
  cd release
  sha256sum pasaDATOS-Windows-x64.exe pasaDATOS-Setup-Windows-x64.exe > SHA256SUMS.txt
)
sha256sum deploy/bin/pasadatos-server-linux-amd64 > deploy/bin/SHA256SUMS.txt

echo "Release generado en release/ y deploy/bin/"
