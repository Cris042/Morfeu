-- Rollback da 011 (task 0023): remove as tabelas do módulo pedido.
DROP TABLE IF EXISTS ingressos;
DROP TABLE IF EXISTS pedido_eventos;
DROP TABLE IF EXISTS pedidos;
