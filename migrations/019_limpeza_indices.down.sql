-- Rollback da 019 (task 0044): remove os índices da limpeza operacional.
DROP INDEX IF EXISTS holds_terminais_atualizado_em_idx;
DROP INDEX IF EXISTS processed_messages_processed_at_idx;
