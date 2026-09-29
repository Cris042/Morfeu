#!/bin/sh
# Task 0007 — usuário de leitura mínima para o postgres-exporter (pg_monitor).
# Executado pelo entrypoint do postgres só na inicialização de volume NOVO.
# A senha vem de PG_MONITOR_PASSWORD (.env.docker-compose, não versionado).
set -eu

if [ -z "${PG_MONITOR_PASSWORD:-}" ]; then
  echo "PG_MONITOR_PASSWORD vazio — usuário de monitoração não criado" >&2
  exit 0
fi

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -v senha="$PG_MONITOR_PASSWORD" <<'SQL'
SELECT 'CREATE ROLE morfeu_monitor LOGIN'
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_monitor')\gexec
ALTER ROLE morfeu_monitor WITH PASSWORD :'senha';
GRANT pg_monitor TO morfeu_monitor;
SQL
