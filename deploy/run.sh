#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."

case "${1:-}" in
    -dev)
        compose=(docker compose -p postly-dev -f docker-compose.yml -f deploy/docker-compose.dev.yml)
        ;;
    -prod)
        compose=(docker compose -p postly -f docker-compose.yml)
        ;;
    *)
        echo "Использование: $0 -dev|-prod [команда docker compose...]" >&2
        exit 1
        ;;
esac
shift

if [ $# -eq 0 ]; then
    set -- up -d --build
fi

exec "${compose[@]}" "$@"
