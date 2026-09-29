-- Task: 0005 — Consumidor idempotente + DLQ (E0b, parte 2/2)
-- PRD: docs/prd/0005-consumidor-idempotente.md
-- Descrição: cria catalogo_filmes_projetados (domínio catalogo, RN03), a
--   projeção alimentada pelo consumo de catalogo.filme_criado (RF07).
--   aplicacoes conta quantas vezes o efeito foi aplicado — prova observável
--   da idempotência (dedup correto => sempre 1). Acesso só por PK (film_id).
--   Sem FK para films de propósito: a projeção é leitura derivada e
--   eventualmente consistente; o evento pode chegar em qualquer ordem.
--   Tabela nova e vazia: sem impacto em dados existentes.
-- Rollback: 004_catalogo_filmes_projetados.down.sql (DROP TABLE).

CREATE TABLE IF NOT EXISTS catalogo_filmes_projetados (
    film_id      BIGINT       PRIMARY KEY,
    titulo       VARCHAR(255) NOT NULL,
    ano          INTEGER,
    aplicacoes   INTEGER      NOT NULL DEFAULT 1,
    projetado_em TIMESTAMPTZ  NOT NULL DEFAULT now()
);
