# PRD 0012 — Importação de filmes do TMDB no backoffice (E2, T2 — marco M2)

- **Task:** docs/tasks/0012-importacao-tmdb.md
- **Branch:** feature/0012-importacao-tmdb
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Permitir que o operador importe filmes reais do TMDB para o catálogo, completando o marco **M2**. Fonte: `docs/refinamentos/E2-catalogo-tmdb.md` §T2 (consensos + pôster por hotlink); doc.md §12 (TMDB: timeout + retry, **sem circuit breaker**, atribuição obrigatória, fallback = cadastro manual); API: Context7 `/websites/developer_themoviedb_reference` (`GET /3/movie/{id}`, `GET /3/search/movie`, `Authorization: Bearer`).

## Escopo

Port e caso de uso no catálogo; adapter HTTP do TMDB; duas rotas de backoffice; configuração do token; métricas e trace da integração; testes.

## Fora de escopo

Cópia/armazenamento do pôster (hotlink — decisão do usuário); sincronização periódica; SPA do backoffice (E5/E9); circuit breaker (decisão da descoberta).

## Requisitos funcionais

- RF01 — Port no catálogo: `FonteTMDB { Detalhar(ctx, tmdbID) (DadosTMDB, error); Buscar(ctx, termo) ([]ResultadoTMDB, error) }`, erros `ErrTMDBNaoEncontrado` e `ErrTMDBIndisponivel`. Injetado por `Servico.ComFonteTMDB(f)`. Sem fonte configurada → `ErrTMDBNaoConfigurado`.
- RF02 — `GET /backoffice/filmes/tmdb?q=<termo>` (Exigir operador): termo de 2 a 100 caracteres (ou 400); até 20 resultados `{tmdb_id, titulo, ano, poster_url}` e a `atribuicao`.
- RF03 — `POST /backoffice/filmes/importar {"tmdb_id": n}` (Exigir operador):
  - Busca o detalhe em pt-BR e mapeia os campos: `title`→titulo, `overview`→sinopse, `runtime`→duracao_min, `release_date`→ano, `poster_path`→`https://image.tmdb.org/t/p/w500<path>` (só se o path tiver o formato esperado), `imdb_id` (só se válido).
  - **`runtime` ausente ou zero → 422** `{"erro":"tmdb_sem_duracao"}`, orientando o cadastro manual.
  - Os dados externos são normalizados: HTML removido, título e sinopse truncados nos limites, depois `validar()` da 0011.
  - **Upsert por `tmdb_id`** numa TX (`INSERT ... ON CONFLICT (tmdb_id) DO UPDATE`). Na criação: **201** e emite `catalogo.filme_criado`. Na reimportação: atualiza os dados, **200**, sem evento e sem desarquivar.
  - Invalida o cartaz depois do commit. Resposta: `{filme, criado, atribuicao}`.
- RF04 — Erros do TMDB: não encontrado → **404** `tmdb_nao_encontrado`; indisponível depois das tentativas → **503** `tmdb_indisponivel`; token não configurado → **503** `tmdb_nao_configurado`.
- RF05 — Adapter `internal/catalogo/tmdb`:
  - `net/http` da stdlib; **host fixo** `https://api.themoviedb.org` (o `BaseURL` alternativo existe só para o httptest); `Authorization: Bearer <TMDB_API_TOKEN>`; timeout de 5 s por tentativa.
  - **Até 3 tentativas**, com backoff exponencial (base 300 ms) + jitter, só em 5xx, erro de rede, timeout e 429 (respeita `Retry-After`, com teto de 5 s). 4xx diferente de 429 não tem retry.
  - JSON malformado → `ErrTMDBIndisponivel`, sem pânico.
  - **Sem circuit breaker** (decisão da descoberta, doc.md §10/§13).
  - Token e URL com credencial nunca vão para erro, log ou span.
- RF06 — Observabilidade: `tmdb_requisicoes_total{operacao,classe_status}` (operacao ∈ {detalhar, buscar}; classe_status ∈ {2xx,4xx,429,5xx,erro}) e `tmdb_requisicao_duracao_segundos{operacao}`; span `tmdb.<operacao>` filho do request. `operacao` e `classe_status` entram na allowlist da telemetria.
- RF07 — Config: `TMDB_API_TOKEN` (opcional; nunca logado). Exemplos de env atualizados.

## Requisitos não funcionais

- RNF01 — Nenhum teste chama a API real: o adapter é testado com `httptest`, e um teste guarda-chuva falha se algum `_test.go` referenciar o host real.
- RNF02 — Fronteiras: o `catalogo` depende do port; o adapter vive em `internal/catalogo/tmdb` e o `main` o conecta.
- RNF03 — Atribuição do TMDB na resposta das rotas de TMDB (termo de uso da API).

## Regras de negócio

- RN01 — Um `tmdb_id` corresponde a no máximo um filme (UNIQUE + upsert).
- RN02 — Sem duração, o filme não entra por importação (o E3 depende dela).

## Critérios de aceite

- [ ] CA01 — Import feliz → 201, campos mapeados, pôster em hotlink, 1 evento na outbox, cartaz invalidado.
- [ ] CA02 — Reimport → 200, mesmo id, dados atualizados, nenhum evento novo; filme arquivado continua arquivado.
- [ ] CA03 — Sem duração → 422; TMDB 404 → 404; TMDB fora (5xx persistente) → 503.
- [ ] CA04 — Adapter: 429 com `Retry-After: 1` → 2ª tentativa ok (conta tentativas); 5xx persistente → exatamente 3 tentativas; 400 → 1 tentativa; timeout; JSON malformado; header `Authorization` correto; o token não aparece em erro nenhum.
- [ ] CA05 — 2 imports concorrentes do mesmo `tmdb_id` → 1 linha, 1 evento.
- [ ] CA06 — Matriz {sem token, cliente, operador} × rotas novas; sem fonte configurada → 503.
- [ ] CA07 — Métricas `tmdb_*` só com labels da allowlist; guarda-chuva contra o host real nos testes; CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Adapter contra httptest (tentativas, Retry-After, timeout, malformado, header) | unit | CA04 |
| Mapeamento/normalização dos dados externos | unit | RF03 |
| Rotas com PG + Redis reais e fonte fake (httptest do adapter real) | integração | CA01–CA03, CA05, CA06 |
| Guarda-chuva do host real + labels | unit | CA07 |

## Plano de implementação

1. Query de upsert + sqlc.
2. `importacao.go` (port, erros, mapeamento, casos de uso) + setter no `Servico`.
3. Adapter `tmdb/cliente.go`.
4. Handler (rotas), `main` (config + wiring), allowlist.
5. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `error-handling`, `golang-observability-opentelemetry`.

## Arquivos que serão criados

- `internal/catalogo/importacao.go`, `internal/catalogo/tmdb/cliente.go`, `internal/catalogo/tmdb/cliente_test.go`, `internal/catalogo/importacao_test.go`
- `docs/prd/0012-importacao-tmdb.md`, `docs/tasks/0012-importacao-tmdb.md`

## Arquivos que serão modificados

- `internal/catalogo/queries.sql`, `db/queries.sql.go`, `db/querier.go`; `internal/catalogo/service.go` (setter), `handler.go` (rotas), `handler_test.go` (integração)
- `cmd/morfeu/main.go`, `internal/config/config.go`, `internal/telemetria/telemetria.go`, `.env.example`, `.env.docker-compose.example`, `lib.md` (TMDB implementado)
- `docs/tasks/README.md`, `plan.md`, `state.md`; fechamento de status: `docs/tasks/0010-…`, `docs/prd/0010-…`, `docs/tasks/0011-…`, `docs/prd/0011-…`

Total previsto: ~25.

## Dependências utilizadas

Nenhuma biblioteca nova (stdlib `net/http`). Serviço externo TMDB já está no `lib.md` (vira "implementado").

## Impactos técnicos

- Duas rotas novas no backoffice; o `TMDB_API_TOKEN` é opcional (sem ele, só as rotas de TMDB respondem 503).
- Uma chamada externa síncrona no request do operador (volume ínfimo).

## Riscos

- TMDB fora ou lento → retry limitado + timeout; o fallback é o cadastro manual da 0011.
- Dados externos maliciosos ou quebrados → normalização + `validar()` + URL do pôster construída por nós (nunca copiada).

## Estratégia de rollback

Reverter o merge. Nenhuma migration (usa `tmdb_id` UNIQUE da 007). Os filmes importados permanecem.
