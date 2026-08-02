#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")"

if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "Se creó deploy/.env. Editalo y ejecutá nuevamente este script." >&2
  exit 1
fi

docker compose up -d --build
echo "Relay escuchando en 127.0.0.1:${PASADATOS_LOCAL_PORT:-8088}. Configurá Nginx/Caddy con HTTPS."
