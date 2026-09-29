# Task 0012 — Importação de filmes do TMDB no backoffice (E2, T2 — marco M2)

- **Data:** 2026-09-29
- **Status:** concluída
- **Branch:** `feature/0012-importacao-tmdb` (da main `dfe0d01`, pós-0011)
- **PRD:** docs/prd/0012-importacao-tmdb.md
- **Item do roadmap:** E2 — Catálogo + TMDB (task 2/2; fecha o épico e o **M2 — operador importa filme real**). Refinamento: `docs/refinamentos/E2-catalogo-tmdb.md` §T2.

## Objetivo

O operador busca um filme no TMDB e o importa para o catálogo: título, sinopse, duração, ano, pôster (hotlink) e IMDb — idempotente por `tmdb_id`, resiliente a falhas do TMDB (timeout + retry, sem circuit breaker) e observável.

## Escopo

Port `FonteTMDB` no catálogo; adapter `internal/catalogo/tmdb` (stdlib, bearer v4, host fixo, retry/backoff/Retry-After, métricas, span); `GET /backoffice/filmes/tmdb?q=` (busca) e `POST /backoffice/filmes/importar` (upsert por `tmdb_id`, evento só na criação, invalidação do cartaz); filme sem duração → 422; atribuição TMDB na resposta; config `TMDB_API_TOKEN` (ausente → 503 nas rotas de TMDB).

## Fora de escopo

Cópia do pôster (decisão: hotlink); SPA (E5); reimportação agendada/sincronização.

## Arquivos esperados

~24 (lista no PRD), inclui o fechamento de status das tasks 0010 e 0011.

## Critérios de aceite

- [ ] Import cria o filme (201) com os dados mapeados e o pôster `https://image.tmdb.org/t/p/w500/...`; reimport devolve 200 com o mesmo id e não duplica evento.
- [ ] Filme sem duração no TMDB → 422 orientando cadastro manual; 404 do TMDB → 404.
- [ ] Adapter: 429 (com `Retry-After`) e 5xx fazem retry até o limite (asserts de nº de tentativas); timeout; JSON malformado; token nunca em erro/log; nenhum teste chama a API real.
- [ ] 2 imports concorrentes do mesmo `tmdb_id` → 1 filme.
- [ ] Matriz de autorização nas rotas novas; sem token TMDB configurado → 503.
- [ ] Métricas `tmdb_*` com labels da allowlist; CI verde.

## Riscos

- Dados externos não confiáveis → mesmos limites/validação da 0011 + normalização.

## Estimativa de impacto

Médio em código (adapter novo), baixo em banco (query de upsert), baixo em infra (env novo), nenhum em usuários.
