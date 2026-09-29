# PRD 0011 — Catálogo: migração `films`→`filmes` + CRUD do operador (E2, T1)

- **Task:** docs/tasks/0011-catalogo-filmes-crud.md
- **Branch:** feature/0011-catalogo-filmes-crud
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Alinhar o catálogo da E0a ao modelo do `doc.md` §5 antes que o E3 crie FK para ele, e dar ao operador o CRUD do backoffice. Fonte: `docs/refinamentos/E2-catalogo-tmdb.md` §T1 e a decisão do pôster por hotlink.

## Escopo

Migration 007; catálogo (queries, service, handler, projeção) em PT; rotas públicas e de backoffice; invalidação síncrona do cache; CLI `criar-filme`; harness de testes da E0a e da outbox usando os arquivos de migration.

## Fora de escopo

Import do TMDB (0012); anti-stampede do cartaz (E5); sessões e FK (E3); auditoria das ações do operador (E9).

## Requisitos funcionais

- RF01 — Migration 007 (up), **preservando os dados**:
  - `films` → `filmes`; colunas renomeadas: `title`→`titulo`, `year`→`ano`, `runtime`→`duracao_min`, `synopsis`→`sinopse`, `created_at`→`criado_em` (passa para `TIMESTAMPTZ`).
  - Colunas novas: `tmdb_id BIGINT UNIQUE`, `arquivado_em TIMESTAMPTZ`, `atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now()`.
  - `id` passa a `GENERATED ALWAYS AS IDENTITY`, reposicionada acima do maior id existente.
  - CHECKs: título com 1–255 caracteres; `duracao_min` entre 1 e 1440 quando presente.
  - O down reverte integralmente para `films`.
- RF02 — Público (sem autenticação):
  - `GET /filmes` lista os não arquivados, com cache read-through na chave `catalogo:filmes:publicos` (TTL de 5 min, herdado da E0a).
  - `GET /filmes/{id}` → 200, ou 404 se não existe ou está arquivado.
  - Corpo em PT: `{id, titulo, sinopse, duracao_min, ano, poster_url, imdb_id, tmdb_id}`.
- RF03 — Backoffice (middleware `Exigir(operador)` injetado pelo `main`; o `catalogo` não importa `autenticacao`):
  - `GET /backoffice/filmes` lista todos, com `arquivado_em`.
  - `POST /backoffice/filmes` → 201; cria o filme e emite `catalogo.filme_criado` na mesma TX.
  - `PUT /backoffice/filmes/{id}` → 200 (substitui os campos editáveis) ou 404.
  - `POST /backoffice/filmes/{id}/arquivar` → 204 (idempotente) ou 404.
  - Sem DELETE físico.
- RF04 — Validação, com 400 `{"erro":"dados_invalidos","campos":[...]}`:
  - `titulo` obrigatório, 1–255 após trim; `sinopse` ≤ 2000.
  - `duracao_min` **obrigatória** na criação e na edição, entre 1 e 1440 (o E3 depende dela).
  - `ano`, se presente, entre 1888 e 2100.
  - `poster_url`, se presente, precisa começar com `https://image.tmdb.org/` (hotlink — decisão do usuário).
  - `imdb_id`, se presente, no formato `tt` + 7 a 10 dígitos.
- RF05 — Cache: criar, editar e arquivar apagam `catalogo:filmes:publicos` depois do commit (DELETE síncrono, consenso do refinamento). Se o delete falhar → log `warn`, e a janela de inconsistência fica limitada pelo TTL.
- RF06 — Evento `catalogo.filme_criado` com payload PT `{id, titulo, ano, duracao_min, sinopse}`, sem versionamento (consenso: único consumidor interno). A projeção (`catalogo.ProjetarFilmeCriado`) passa a ler o payload PT. Editar e arquivar não emitem evento (sem consumidor).
- RF07 — CLI `criar-filme -titulo -duracao [-sinopse] [-ano]`: `-duracao` passa a ser obrigatória (mesma regra do RF04) e usa o mesmo caso de uso do backoffice.
- RF08 — `cache.Cache` ganha `Delete(ctx, chave)`.

## Requisitos não funcionais

- RNF01 — Fronteiras: o `catalogo` não importa `autenticacao`/`identidade`/driver (depguard `catalogo-domain` inalterado); o backoffice recebe o middleware por parâmetro.
- RNF02 — Testes com PG e Redis reais. Os testes da E0a (`internal/integration*_test.go`) e da outbox passam a aplicar os **arquivos de migration** (001 + 007 e as demais), em vez de DDL inline duplicado.
- RNF03 — Contrato: `Cache-Control: public, max-age=300` mantido no `GET /filmes`; o 404 é genérico.

## Regras de negócio

- RN01 — Filme arquivado some do cartaz e do detalhe público, mas continua existindo (FK futura de sessão, histórico).
- RN02 — Todo filme novo tem duração (pré-condição da regra de não-conflito do E3).

## Critérios de aceite

- [ ] CA01 — Migration: up aplica em base com os 10 seeds da 001 e preserva todos (títulos, anos, durações); o próximo id gerado é maior que 10; down→up volta ao mesmo estado; o job `migrations` do CI passa.
- [ ] CA02 — 2 criações concorrentes → ids distintos, sem erro.
- [ ] CA03 — Público: lista sem arquivados, detalhe 200/404, JSON em PT.
- [ ] CA04 — Matriz: {sem token, cliente, operador, expirado} × {GET, POST, PUT, arquivar} do backoffice → 401/403/2xx exatos.
- [ ] CA05 — Validação: um caso por campo, com o campo correto em `campos`.
- [ ] CA06 — Cache: depois de popular, criar, editar e arquivar, cada operação apaga a chave no Redis real; a leitura seguinte repopula sem o arquivado.
- [ ] CA07 — Regressão: suítes da E0a (cache read-through, TTL, fallback) e da outbox/consumer/walking skeleton verdes com o payload PT.
- [ ] CA08 — CI verde (lint, `-race`, sqlc, migrations).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Migration com os seeds (up/down/up) | integração (PG) | CA01 |
| Validação por campo | unit | CA05 |
| CRUD + matriz + concorrência + cache pelas rotas | integração (PG + Redis) | CA02–CA06 |
| Suítes E0a/outbox com os arquivos de migration | integração | CA07 |

## Plano de implementação

1. Migration 007 + `sqlc.yaml` + queries.
2. `cache.Delete`.
3. Catálogo: `service.go` (casos de uso + validação + invalidação), `handler.go` (público + backoffice), `projecao.go`, `errors.go`.
4. `main.go` (rotas do backoffice, CLI).
5. Harness dos testes existentes + integração nova; lint; `-race`; gate.

**Skills de apoio (§4.4):** `database-migrations`, `golang-testing`.

## Arquivos que serão criados

- `migrations/007_filmes.up.sql`, `migrations/007_filmes.down.sql`
- `internal/catalogo/errors.go`
- `docs/prd/0011-catalogo-filmes-crud.md`, `docs/tasks/0011-catalogo-filmes-crud.md`

## Arquivos que serão modificados

- `sqlc.yaml`; `internal/catalogo/queries.sql`, `internal/catalogo/db/{models.go,querier.go,queries.sql.go}`
- `internal/catalogo/service.go`, `handler.go`, `projecao.go`, `service_test.go` (validação — unit), `handler_test.go` (vira a suíte de integração do CRUD: matriz, concorrência, cache — tag `integration`)
- `internal/cache/redis.go`
- `cmd/morfeu/main.go`, `cmd/morfeu/main_test.go`
- `internal/integration_test.go`, `internal/integration_cache_test.go`, `internal/outbox/relay_integration_test.go`, `internal/outbox/consumidor_integration_test.go`
- `docs/tasks/README.md`, `plan.md`, `state.md`; do fechamento da 0010: `docs/tasks/0010-…`, `docs/prd/0010-…`; já no branch: `docs/refinamentos/E2-catalogo-tmdb.md`, `docs/refinamentos/README.md`

Total previsto: ~31, que passa do teto de 30. **Mitigação:** o fechamento de status da 0010 (2 arquivos) sai desta branch (é revertido aqui e aplicado junto com a 0012), o que deixa ~29.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- O contrato JSON de `GET /filmes` passa de EN para PT (ainda não há consumidor; o SPA nasce no E5).
- A chave de cache muda de `films:list` para `catalogo:filmes:publicos` (a antiga expira sozinha pelo TTL).
- A CLI passa a exigir `-duracao`.

## Riscos

- A migração com dados quebrar o rollback → up/down/up com os seeds no teste e no CI.
- O harness dos testes antigos mascarar a regressão → os asserts existentes são mantidos (só os nomes mudam).

## Estratégia de rollback

Reverter o merge; `migrate down 1` (007 down: remove as colunas novas, a identity e os CHECKs; renomeia de volta para `films`/EN; `criado_em`→`created_at TIMESTAMP`). Os dados são preservados nos dois sentidos.
