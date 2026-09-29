# PRD 0014 — Sessões: leitura pública, mapa de assentos e cache (E3, T2)

- **Task:** docs/tasks/0014-sessoes-leitura-publica.md
- **Branch:** feature/0014-sessoes-leitura-publica
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Fechar o E3 com a leitura pública das sessões e o mapa de assentos que o E4 (reserva) e o E5 (SPA) consomem. Fonte: `docs/refinamentos/E3-sessoes-salas.md` §T2; auditoria da 0013 (não-bloqueante nº 2).

## Escopo

Duas rotas públicas; filtros na listagem do operador; cache Redis com invalidação; ajuste da comparação de layout.

## Fora de escopo

Ocupação de assentos (holds — E4); anti-stampede do cache (E5/E12, medido pelo k6); exibição em fuso local (SPA).

## Requisitos funcionais

- RF01 — `GET /filmes/{id}/sessoes` (público): sessões `agendada` com `inicio > agora` do filme, em ordem de `inicio`, até 100. Campos `{id, sala_id, sala_nome, inicio, fim, preco_centavos}` (horários em UTC). **Nenhum campo de filme** — o SPA já tem o filme (fronteira ADR 0003). Filme sem sessões → `[]`.
- RF02 — Cache read-through `sessao:filme:{id}:futuras`, TTL 60 s. Na leitura do cache, as sessões com `inicio <= agora` são removidas: a defasagem do TTL nunca mostra sessão já iniciada.
- RF03 — Invalidação síncrona após o commit: criar sessão apaga a chave do filme; cancelar sessão apaga a chave do filme da sessão (o cancelamento passa a devolver o `filme_id`). Falha no delete → `warn`, com o TTL limitando a defasagem.
- RF04 — `GET /sessoes/{id}/mapa` (público): para sessão `agendada` com `inicio > agora` → `{sessao_id, sala_id, sala_nome, fileiras, colunas, assentos: [{codigo, fileira, coluna, pcd}]}` (base do E4/E5; sem ocupação ainda). Cancelada, passada ou inexistente → 404.
- RF05 — `GET /backoffice/sessoes?sala_id=&data=AAAA-MM-DD`: filtros opcionais por sala e por dia (UTC); filtro inválido → 400.
- RF06 — As rotas públicas do `sessao` são registradas em **todos** os modos (como o cartaz); o backoffice, só em `api|all`.
- RF07 — Comparação de layout para a imutabilidade (0013 RN03) independente da ordem de `vaos`/`pcd` (auditoria 0013).
- RF08 — O `sessao` recebe `cache.Cache` (plataforma); depguard `sessao-domain` passa a permitir `internal/cache`.

## Requisitos não funcionais

- RNF01 — As consultas públicas usam o índice parcial `(filme_id, inicio) WHERE status='agendada'` (0013).
- RNF02 — Testes com PG e Redis reais; relógio do serviço injetável.

## Regras de negócio

- RN01 — O público só vê sessões agendadas que ainda não começaram.

## Critérios de aceite

- [ ] CA01 — Lista pública: futuras e agendadas do filme, em ordem; cancelada, passada e de outro filme ausentes; JSON sem `titulo`/`filme_id`.
- [ ] CA02 — Cache: a chave existe depois da leitura; criar ou cancelar sessão a apaga (Redis real); uma sessão que "começa" durante o TTL (relógio avançado) some da resposta servida do cache.
- [ ] CA03 — Mapa: códigos corretos (vão ausente, PCD marcado); 404 para cancelada, passada e inexistente.
- [ ] CA04 — Filtros do operador: por sala, por dia e combinados; `data` inválida → 400.
- [ ] CA05 — Layout reordenado não dispara `layout_em_uso`; layout de fato diferente ainda dispara (unit + integração).
- [ ] CA06 — Rotas públicas sem autenticação; CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Comparação de layout | unit | CA05 |
| Rotas públicas + cache + mapa + filtros | integração (PG + Redis) | CA01–CA04, CA06 |

## Plano de implementação

1. Queries (lista pública, mapa, filtros, cancelar com `filme_id`) + sqlc.
2. Service (cache, refiltragem, invalidação, mapa, filtros, `mesmoLayout` normalizado).
3. Handler (rotas públicas) + `main` (registro em todos os modos) + depguard.
4. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `golang-testing`, `postgres-patterns`.

## Arquivos que serão criados

- `docs/prd/0014-sessoes-leitura-publica.md`, `docs/tasks/0014-sessoes-leitura-publica.md`

## Arquivos que serão modificados

- `internal/sessao/queries.sql`, `internal/sessao/db/queries.sql.go`
- `internal/sessao/service.go`, `handler.go`, `service_test.go`, `handler_test.go`
- `cmd/morfeu/main.go`, `.golangci.yml`
- `docs/tasks/README.md`, `plan.md`, `state.md`, `docs/roadmap.md`; fechamento de status: `docs/tasks/0012-…`, `docs/prd/0012-…`, `docs/tasks/0013-…`, `docs/prd/0013-…`

Total previsto: ~18.

## Dependências utilizadas

Nenhuma nova (`internal/cache` existente).

## Impactos técnicos

- Duas rotas públicas novas (sem auth, cacheadas).
- A listagem do backoffice ganha filtros (compatível: sem filtro, o comportamento é o anterior).

## Riscos

- Colisão de rota `GET /filmes/:id` (catálogo) × `GET /filmes/:id/sessoes` (sessão) → paths distintos no roteador do Echo; teste cobre as duas.

## Estratégia de rollback

Reverter o merge; nenhuma migration.
