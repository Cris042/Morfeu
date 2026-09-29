-- Rollback da 006 (task 0010): remove refresh_token (índices em cascata).
DROP TABLE IF EXISTS refresh_token;
