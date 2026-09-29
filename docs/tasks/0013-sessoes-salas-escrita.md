# Task 0013 — Sessões e salas: escrita, layout e regra de não-conflito (E3, T1)

- **Data:** 2026-09-29
- **Status:** concluída
- **Branch:** `feature/0013-sessoes-salas-escrita` (da main `1514361`, pós-E2)
- **PRD:** docs/prd/0013-sessoes-salas-escrita.md
- **Item do roadmap:** E3 — Sessões e salas (task 1/2). Refinamento: `docs/refinamentos/E3-sessoes-salas.md` §T1 (limpeza 20 min; só API).

## Objetivo

Módulo `internal/sessao`: salas com layout JSON validado (códigos de assento determinísticos), sessões com snapshot da duração do filme (porta síncrona ao catálogo) e **não-conflito garantido pelo banco** (`EXCLUDE USING gist`), backoffice por API.

## Escopo

Refinamento E3 §T1: migration 008 (btree_gist, salas, sessoes, EXCLUDE, índices), módulo `sessao` (layout, service, handler, erros; tradução 23P01 em `sessao/db/erros.go`), porta `DuracaoFilmeAtivo` implementada pelo catálogo, wiring no main (middleware de operador, id do operador para log, métrica de conflitos), depguard `sessao-domain`, testes.

## Fora de escopo

Leitura pública e cache (0014); telas (E5/E9); hold/mapa com ocupação (E4); cancelamento com ingressos vendidos (E9).

## Arquivos esperados

29 (lista no PRD).

## Critérios de aceite

- [ ] Migration 008 up→down→up; EXCLUDE ativa só para `agendada`.
- [ ] Layout validado (limites, duplicatas, PCD em vão, chave desconhecida, 64 KB) e códigos determinísticos.
- [ ] Sessão: `fim = início + duração + 20 min`; passado → 400; preço fora da faixa → 400; filme inexistente/arquivado → 422; sala inexistente → 422.
- [ ] Conflito → 409 com o horário conflitante; encostada no fim permitida; outra sala permitida; cancelada libera o horário; 2 criações concorrentes → exatamente 1.
- [ ] Layout imutável com sessão agendada futura (409); nome editável.
- [ ] Matriz de autorização; CI verde.

## Riscos

- 29 arquivos → lista fechada.

## Estimativa de impacto

Alto em código (módulo novo), médio em banco (extensão + 2 tabelas + EXCLUDE), nenhum em infra/usuários.
