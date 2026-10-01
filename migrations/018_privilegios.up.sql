-- Task: 0042 — Hardening do compose, senha do Redis e roles do PG (E11 T1)
-- PRD: docs/prd/0042-hardening-roles.md (ADR 0013)
-- Descrição: com os roles de menor privilégio presentes (criados pelo init
--   configs/postgres/02-roles.sh, nunca por migration — senha não entra aqui),
--   o app perde DELETE/TRUNCATE/TRIGGER na trilha de auditoria e a purga de 12
--   meses passa a ser do morfeu_purge (SELECT + DELETE). Sem os roles (dev/CI,
--   usuário único) é no-op. Idempotente: REVOKE/GRANT repetidos são inócuos.
--   Roda como morfeu_migrator (dono da tabela) em produção.
-- Rollback: 018_privilegios.down.sql (devolve os privilégios ao app).

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_app') THEN
        REVOKE DELETE, TRUNCATE, TRIGGER ON eventos_auditoria FROM morfeu_app;
    END IF;
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_purge') THEN
        GRANT SELECT, DELETE ON eventos_auditoria TO morfeu_purge;
    END IF;
END
$$;
