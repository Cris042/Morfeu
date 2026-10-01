-- Rollback da 017 (task 0037): descarta a trilha de auditoria.
DROP TABLE IF EXISTS eventos_auditoria;
DROP FUNCTION IF EXISTS eventos_auditoria_imutavel();
