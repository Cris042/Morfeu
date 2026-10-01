#!/bin/sh
# Task 0042 (ADR 0013) — roles de menor privilégio do PostgreSQL.
# Executado pelo entrypoint do postgres só na inicialização de volume NOVO.
# Cada role só nasce se a env de senha correspondente estiver presente
# (senha nunca em migration nem em arquivo versionado):
#   PG_MIGRATOR_PASSWORD -> morfeu_migrator (dono do banco; só ele roda DDL)
#   PG_APP_PASSWORD      -> morfeu_app      (DML; não é dono de nada)
#   PG_PURGE_PASSWORD    -> morfeu_purge    (SELECT/DELETE da trilha, via migration 018)
#   PG_BACKUP_PASSWORD   -> morfeu_backup   (pg_read_all_data, somente leitura)
# Sem nenhuma env, nada muda (dev/CI seguem com o usuário único).
# Volume existente: ver docs/observabilidade.md ("Roles em volume existente").
set -eu

psql_admin() {
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" "$@"
}

criar_role() {
  role="$1"
  senha="$2"
  if [ -z "$senha" ]; then
    echo "senha de $role vazia — role não criado" >&2
    return 0
  fi
  psql_admin -v role="$role" -v senha="$senha" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN', :'role')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'role')\gexec
SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', :'role', :'senha')\gexec
SQL
}

criar_role morfeu_migrator "${PG_MIGRATOR_PASSWORD:-}"
criar_role morfeu_app "${PG_APP_PASSWORD:-}"
criar_role morfeu_purge "${PG_PURGE_PASSWORD:-}"
criar_role morfeu_backup "${PG_BACKUP_PASSWORD:-}"

# Concessões. Cada bloco só vale se o role existe. Sem o migrator, os default
# privileges não fazem sentido (o dono continua sendo o usuário único).
psql_admin <<'SQL'
DO $$
DECLARE
  r text;
BEGIN
  IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_migrator') THEN
    -- PG16: o schema public pertence a pg_database_owner, então o dono do
    -- banco passa a poder criar objetos nele.
    EXECUTE format('ALTER DATABASE %I OWNER TO morfeu_migrator', current_database());
  END IF;

  FOREACH r IN ARRAY ARRAY['morfeu_migrator', 'morfeu_app', 'morfeu_purge', 'morfeu_backup'] LOOP
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = r) THEN
      EXECUTE format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), r);
      EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', r);
    END IF;
  END LOOP;

  IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_migrator')
     AND EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_app') THEN
    -- Tabela/sequência criada pelo migrator já nasce acessível ao app.
    ALTER DEFAULT PRIVILEGES FOR ROLE morfeu_migrator IN SCHEMA public
      GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO morfeu_app;
    ALTER DEFAULT PRIVILEGES FOR ROLE morfeu_migrator IN SCHEMA public
      GRANT USAGE, SELECT ON SEQUENCES TO morfeu_app;
  END IF;

  IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_backup') THEN
    GRANT pg_read_all_data TO morfeu_backup;
  END IF;
END
$$;
SQL
