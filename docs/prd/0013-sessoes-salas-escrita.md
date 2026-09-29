# PRD 0013 — Sessões e salas: escrita, layout e regra de não-conflito (E3, T1)

- **Task:** docs/tasks/0013-sessoes-salas-escrita.md
- **Branch:** feature/0013-sessoes-salas-escrita
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Criar o módulo `sessao` (transaction script, ADR 0005) com as salas e a programação de sessões pelo operador, garantindo no banco que duas sessões agendadas nunca se sobrepõem na mesma sala (doc.md §6.3). Fonte: `docs/refinamentos/E3-sessoes-salas.md` §T1 e as decisões do usuário (limpeza de 20 min; só API).

## Escopo

Migration 008; módulo `internal/sessao` (layout, service, handler, erros e tradução do erro de exclusão); porta de duração do filme implementada pelo catálogo; wiring; depguard; testes.

## Fora de escopo

Leitura pública, mapa público e cache (0014); telas do operador (E5/E9); holds e ocupação de assentos (E4); cancelamento de sessão com ingressos (E9).

## Requisitos funcionais

- RF01 — Migration 008:
  - `CREATE EXTENSION IF NOT EXISTS btree_gist`.
  - `salas`: `id` IDENTITY, `nome` VARCHAR(80) UNIQUE, `layout` JSONB, timestamps.
  - `sessoes`: `id` IDENTITY, `filme_id` FK→`filmes`, `sala_id` FK→`salas`, `inicio`/`fim` TIMESTAMPTZ, `duracao_min` (1–1440), `preco_centavos` (100–100000), `status` (`agendada`|`cancelada`, padrão `agendada`), timestamps.
  - Constraints: `CHECK (fim > inicio)` e **`EXCLUDE USING gist (sala_id WITH =, tstzrange(inicio, fim, '[)') WITH &&) WHERE (status = 'agendada')`**.
  - Índices em `sala_id` e em `(filme_id, inicio) WHERE status = 'agendada'`.
  - O down remove as tabelas e mantém a extensão (inofensiva e compartilhável).
- RF02 — Layout: JSON `{"fileiras": n, "colunas": n, "vaos": [{"fileira":"A","coluna":5}], "pcd": [...]}`.
  - Validação em Go: schema estrito (chave desconhecida → 400); 1–26 fileiras (letras A–Z); 1–50 colunas; posições dentro da grade; sem duplicatas; PCD não pode ficar em vão; corpo ≤ 64 KB.
  - `Assentos(layout)` devolve, em ordem determinística, os códigos (`F7`), e cada assento traz fileira, coluna e se é PCD. Vãos não geram assento.
- RF03 — Salas (backoffice):
  - `POST /backoffice/salas` → 201; nome repetido → 409.
  - `GET /backoffice/salas`.
  - `PUT /backoffice/salas/{id}` altera nome e/ou layout. Mudar o layout com ≥ 1 sessão `agendada` futura na sala → **409 `layout_em_uso`**; mudar só o nome é permitido.
- RF04 — Sessões (backoffice): `POST /backoffice/sessoes {filme_id, sala_id, inicio (RFC3339), preco_centavos}`. Validações, nesta ordem:
  1. `inicio` no passado → 400.
  2. Preço fora de 100–100000 → 400.
  3. Sala inexistente → 422 `sala_inexistente`.
  4. **Porta** `DuracaoFilmeAtivo(filmeID)` → se não encontrado, arquivado ou sem duração → 422 `filme_indisponivel`.
  5. `fim = inicio + duracao_min + 20 min` (`IntervaloLimpeza`, relógio injetável).
  6. INSERT. Violação da EXCLUDE → **409 `conflito_horario`** com `{inicio, fim}` da sessão conflitante.
- RF05 — Sessões (backoffice): `GET /backoffice/sessoes` (as 200 mais recentes por `inicio`) e `POST /backoffice/sessoes/{id}/cancelar` → 204, idempotente, ou 404. A cancelada sai da EXCLUDE e libera o horário.
- RF06 — Porta: `sessao.FonteFilmes { DuracaoFilmeAtivo(ctx, filmeID) (int32, bool, error) }`, implementada por `catalogo.Servico` (query própria do catálogo; o `sessao` nunca lê `filmes`). O `main` injeta. A FK `sessoes.filme_id` é integridade declarada — não é leitura.
- RF07 — Observabilidade e auditoria mínima:
  - Callback `AoConflito(ctx)` injetado pelo `main`, que incrementa `sessao_conflitos_total` (o domínio não conhece OTel).
  - Log `info` com `operador_id` + ação + id (sem PII) em criar/editar sala e em criar/cancelar sessão. O `operador_id` vem de uma função injetada pelo `main` (`autenticacao.UsuarioID`), porque o `sessao` não importa autenticação.
- RF08 — Tradução do erro: `internal/sessao/db/erros.go`, arquivo **não gerado** e único lugar com `pgconn`, expõe `EhConflitoDeHorario(err)` (SQLSTATE 23P01 da constraint `sessoes_sem_conflito`) e `EhNomeDuplicado(err)` (23505 em `salas_nome_key`).

## Requisitos não funcionais

- RNF01 — Fronteiras (ADR 0003): depguard `sessao-domain` (strict, igual ao `catalogo-domain`); `pgconn` só no arquivo de erros do pacote `db`; nenhum import de `catalogo`, `autenticacao` ou OTel no domínio.
- RNF02 — Tempo injetado nos testes de unidade (ADR 0006); integração com PG real para a EXCLUDE.
- RNF03 — Timestamps em UTC (timestamptz); a exibição no fuso do cinema fica para a borda (E5).

## Regras de negócio

- RN01 — Duas sessões `agendada` na mesma sala não se sobrepõem, considerando a limpeza de 20 min (intervalo `[inicio, fim)`: encostar no fim é permitido).
- RN02 — A sessão guarda a duração vigente na criação (snapshot); editar o filme depois não move a sessão.
- RN03 — O layout é imutável enquanto houver sessão agendada futura na sala.

## Critérios de aceite

- [ ] CA01 — Migration 008 up→down→up (CI).
- [ ] CA02 — Layout: tabela de casos válidos e inválidos (1 por regra); códigos determinísticos e sem os vãos.
- [ ] CA03 — `fim` calculado corretamente (unit, relógio fixo); passado → 400; preço fora da faixa → 400.
- [ ] CA04 — Conflitos (integração): parcial no início, parcial no fim e contida → 409 com o horário; encostada no fim → 201; outra sala → 201; cancelar libera o horário.
- [ ] CA05 — 2 criações concorrentes conflitantes → exatamente 1 × 201 e 1 × 409.
- [ ] CA06 — Porta real do catálogo: filme arquivado → 422; inexistente → 422; sala inexistente → 422.
- [ ] CA07 — Layout imutável com sessão futura → 409; nome editável; sem sessão → layout editável.
- [ ] CA08 — Matriz {sem token, cliente, operador} × rotas de backoffice; métrica/callback de conflito chamada.
- [ ] CA09 — CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Validação de layout e códigos | unit | CA02 |
| Cálculo de fim e validações de entrada | unit (relógio fixo) | CA03 |
| Rotas com PG real (migrations 001/007/008) + catálogo real como porta | integração | CA04–CA08 |

## Plano de implementação

1. Migration 008 + `sqlc.yaml` + queries (sessao e porta no catálogo).
2. `layout.go`, `errors.go`, `service.go`, `handler.go`, `db/erros.go`.
3. `catalogo.Servico.DuracaoFilmeAtivo`.
4. `main.go` (wiring, callback de métrica, id do operador) e depguard.
5. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `postgres-patterns`, `golang-testing`.

## Arquivos que serão criados

- `migrations/008_salas_sessoes.up.sql`, `migrations/008_salas_sessoes.down.sql`
- `internal/sessao/queries.sql`, `internal/sessao/db/{db.go,models.go,queries.sql.go}` (gerados), `internal/sessao/db/erros.go`
- `internal/sessao/layout.go`, `service.go`, `handler.go`, `errors.go`
- `internal/sessao/layout_test.go`, `internal/sessao/service_test.go`, `internal/sessao/handler_test.go` (integração)
- `docs/prd/0013-sessoes-salas-escrita.md`, `docs/tasks/0013-sessoes-salas-escrita.md`

## Arquivos que serão modificados

- `sqlc.yaml`; `internal/catalogo/queries.sql`, `internal/catalogo/db/{queries.sql.go,querier.go}`, `internal/catalogo/service.go` (porta)
- `cmd/morfeu/main.go`, `.golangci.yml`
- `docs/tasks/README.md`, `plan.md`, `state.md`; já no branch: `docs/refinamentos/E3-sessoes-salas.md`, `docs/refinamentos/README.md`, `docs/roadmap.md`

Total previsto: 29 (o fechamento de status da 0012 vai para a 0014).

## Dependências utilizadas

Nenhuma biblioteca nova. A extensão `btree_gist` faz parte do contrib do PostgreSQL (já presente na `postgres:16-alpine`).

## Impactos técnicos

- Extensão nova no banco (em banco gerenciado, pode exigir privilégio — hoje é self-hosted).
- O catálogo ganha um método de leitura para a porta.

## Riscos

- A EXCLUDE depende do `fim` persistido estar correto → cálculo testado em unit e em integração.
- `CREATE EXTENSION` sem privilégio num banco gerenciado futuro → comentado na migration.

## Estratégia de rollback

Reverter o merge; `migrate down 1` (008 down: `DROP TABLE sessoes, salas`; a extensão permanece). Nenhuma tabela existente é alterada.
