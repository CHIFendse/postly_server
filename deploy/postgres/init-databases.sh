#!/bin/bash
set -euo pipefail

for schema in /schemas/*.sql; do
    db=$(basename "$schema" .sql)
    echo "init-databases: создаю БД \"$db\""
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
        -c "CREATE DATABASE \"$db\""
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$db" -f "$schema"
done
