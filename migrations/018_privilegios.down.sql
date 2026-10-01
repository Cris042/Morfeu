-- Rollback da 018 (task 0042): devolve ao app o DELETE que ele tinha pelos
-- default privileges (TRUNCATE/TRIGGER nunca foram dele) e revoga os do
-- morfeu_purge. No-op sem os roles.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_purge') THEN
        REVOKE SELECT, DELETE ON eventos_auditoria FROM morfeu_purge;
    END IF;
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'morfeu_app') THEN
        GRANT DELETE ON eventos_auditoria TO morfeu_app;
    END IF;
END
$$;
