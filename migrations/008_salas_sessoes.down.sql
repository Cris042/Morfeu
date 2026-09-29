-- Rollback da 008 (task 0013): remove sessões e salas. A extensão btree_gist
-- é mantida (inofensiva; pode ser usada por outros objetos).
DROP TABLE IF EXISTS sessoes;
DROP TABLE IF EXISTS salas;
