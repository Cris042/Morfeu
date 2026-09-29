# Task 0011 — Catálogo: migração `films`→`filmes` + CRUD do operador (E2, T1)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0011-catalogo-filmes-crud` (da main `5d2ca7f`, pós-E1)
- **PRD:** docs/prd/0011-catalogo-filmes-crud.md
- **Item do roadmap:** E2 — Catálogo + TMDB (task 1/2). Refinamento: `docs/refinamentos/E2-catalogo-tmdb.md` §T1.

## Objetivo

Levar o catálogo herdado da E0a ao modelo do `doc.md` antes que o E3 crie FK: tabela `filmes` em PT com PK por IDENTITY, dados preservados, CRUD do operador no backoffice (criar, editar, arquivar — sem delete físico), leitura pública excluindo arquivados e invalidação síncrona do cache do cartaz.

## Escopo

Refinamento E2 §T1: migration 007 (rename + IDENTITY + colunas novas + CHECKs, down completo), queries e service/handler do catálogo em PT, rotas públicas e `/backoffice/filmes` sob `Exigir(operador)`, `cache.Delete`, evento `catalogo.filme_criado` com payload PT (consumer atualizado), CLI `criar-filme` exigindo duração, testes E0a/outbox passando a usar os arquivos de migration.

## Fora de escopo

Import do TMDB (0012); anti-stampede do cartaz (E5); sessões (E3).

## Arquivos esperados

~30 (no limite; lista fechada no PRD).

## Critérios de aceite

- [ ] Migration 007 up→down→up com os 10 seeds preservados; id novo sem colisão sob concorrência.
- [ ] `GET /filmes` e `GET /filmes/{id}` públicos, em PT, sem arquivados.
- [ ] Backoffice: criar, editar, arquivar, listar (com arquivados) — matriz {sem token, cliente, operador, expirado} × rotas.
- [ ] Validação (título, sinopse, duração 1–1440, ano, pôster só `image.tmdb.org`, imdb_id).
- [ ] Cache do cartaz apagado após criar/editar/arquivar (verificado no Redis).
- [ ] Walking skeleton (CLI → outbox → consumer → projeção) verde com o payload PT; CI verde.

## Riscos

- Teto de 30 arquivos → lista fechada; testes E0a reescritos só no harness de schema.
- Migração com dados → up/down testados com os seeds.

## Estimativa de impacto

Alto em código (catálogo inteiro + testes da E0a), médio em banco (rename + identity com dados), nenhum em infra; contrato JSON do cartaz muda para PT (sem consumidor ainda).
