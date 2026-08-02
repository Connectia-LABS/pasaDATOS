#!/usr/bin/env bash
set -Eeuo pipefail

cd "$(dirname "$0")"

if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "Se creó deploy/.env. Editalo con tu dominio y la red de Traefik antes de continuar." >&2
  exit 1
fi

set -a
# shellcheck disable=SC1091
source .env
set +a

: "${PASADATOS_DOMAIN:?Falta PASADATOS_DOMAIN en .env}"
: "${PASADATOS_PUBLIC_URL:?Falta PASADATOS_PUBLIC_URL en .env}"

docker compose -f docker-compose.yml -f docker-compose.traefik.yml up -d --build

echo "pasaDATOS relay desplegado en ${PASADATOS_PUBLIC_URL}"
