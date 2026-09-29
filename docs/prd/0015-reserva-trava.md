# PRD 0015 — Reserva: trava de assento (holds) + sweeper (E4, T1)

- **Task:** docs/tasks/0015-reserva-trava.md
- **Branch:** feature/0015-reserva-trava
- **Data:** 2026-09-29
- **Status:** concluído

## Objetivo

Criar o módulo `reserva` (DDD tático, ADR 0005) com a trava de assentos no PostgreSQL definida no ADR 0008: quando dois donos disputam o mesmo assento, exatamente um vence; o hold vale 10 min, pode ser estendido uma vez e expira de forma lazy, com um sweeper de higiene. Fontes: `docs/refinamentos/E4-reserva.md` §T1 e as decisões do usuário (cookie HttpOnly + anti-CSRF; 6 holds por dono; 30/min por IP e 20/min por dono).

## Escopo

Migration 009; módulo `internal/reserva` (VOs + aggregate, token de carrinho, repository, serviço, handler, sweeper, erros, tradução de erro do driver); porta `FonteSessoes` implementada pelo `sessao`; wiring no `main` (rotas em api|all, sweeper no worker, métricas, limitadores); depguard; testes.

## Fora de escopo

`GET /sessoes/{id}/ocupacao`, cache e alertas (0016); conversão hold → ingresso (E6); UI (E5); purge de holds terminais (hardening); vínculo carrinho ↔ conta (E6/E8).

## Requisitos funcionais

- RF01 — Migration 009 `holds`:
  - `id uuid PK`, `sessao_id bigint NOT NULL REFERENCES sessoes(id)`, `assento_codigo varchar(3) NOT NULL CHECK (assento_codigo ~ '^[A-Z][1-9][0-9]?$')`, `dono_hash bytea NOT NULL CHECK (octet_length = 32)`, `status varchar(10) NOT NULL CHECK IN ('ativo','liberado','expirado','convertido')`, `expires_at timestamptz NOT NULL`, `extensoes_usadas smallint NOT NULL DEFAULT 0 CHECK 0..1`, `criado_em`, `atualizado_em`.
  - **`CREATE UNIQUE INDEX holds_assento_ativo ON holds (sessao_id, assento_codigo) WHERE status = 'ativo'`**.
  - Índices parciais `(expires_at) WHERE status='ativo'` (roubo/sweeper) e `(dono_hash) WHERE status='ativo'` (teto/meus holds).
  - O down derruba a tabela.
- RF02 — Porta `reserva.FonteSessoes { AssentosDaSessaoAberta(ctx, sessaoID) (codigos []string, ok bool, err error) }`. `ok=false` quando a sessão não existe, foi cancelada ou já começou. Implementada por `sessao.Servico` reusando a query do mapa. O `main` injeta a porta, e a `reserva` nunca lê `sessoes`/`salas`.
- RF03 — Token de carrinho (dono):
  - Gerado com 32 bytes de `crypto/rand`, em base64url sem padding (43 caracteres). Em repouso, só o SHA-256 (`dono_hash`).
  - Cookie `morfeu_carrinho`: HttpOnly, Secure, SameSite=Strict, Path `/`, sem Max-Age.
  - Cookie ausente ou com formato inválido na trava → emite um novo. Nas demais rotas, cookie ausente = sem holds.
  - O token nunca aparece em log nem em resposta JSON.
- RF04 — `POST /sessoes/{id}/holds {"assentos": ["F7", ...]}` (público, exige `X-Requested-With: morfeu`, corpo ≤ 4 KB, JSON estrito):
  1. Rate limit (RF08) → 429 `muitas_requisicoes`.
  2. Validação em memória (aggregate): 1–6 códigos, formato válido, sem repetição → senão 400 `dados_invalidos`.
  3. Porta: sessão indisponível → 404; código fora do layout → 400 `dados_invalidos` (`campos: ["assentos"]`).
  4. TX:
     - `pg_advisory_xact_lock(namespace, chave derivada do dono_hash)`.
     - Lê os holds vivos do dono. Assentos da mesma sessão já travados pelo próprio dono são devolvidos como estão (idempotente, sem renovar prazo).
     - Vivos + novos > 6 → 409 `limite_holds`.
     - Upsert dos novos **em ordem crescente de código** (ADR 0008), com `expires_at = agora + 10 min`.
     - Qualquer upsert sem linha → rollback e 409 `assento_indisponivel {"assentos": [recusados]}`.
  5. 201 `{"holds": [{id, sessao_id, assento, expira_em, extensoes_usadas}]}` + cookie, quando emitido.
  - Deadlock 40P01 → repete a TX até 3 vezes (defesa extra à ordenação; SQLSTATE vai para o log).
- RF05 — `GET /holds`: holds vivos do dono (todas as sessões), ordenados por expiração. Sem cookie → `[]`.
- RF06 — `POST /holds/{id}/estender` (anti-CSRF, rate limit):
  - O aggregate carrega o hold vivo do dono e aplica `Estender(agora)`: +10 min sobre `expires_at`, uma única vez.
  - O UPDATE tem a guarda `status='ativo' AND expires_at > agora AND extensoes_usadas = 0`.
  - Resultados: 200 com o hold; já estendido → 409 `extensao_esgotada`; de outro dono, vencido ou inexistente → 404.
- RF07 — `DELETE /holds/{id}` (anti-CSRF): hold vivo do dono → `liberado`, 204. Caso contrário → 404.
- RF08 — Rate limit: `POST /sessoes/{id}/holds` e `/estender` contam por IP (30/min) e por dono (20/min, só com cookie válido), usando o limitador do E1 (Redis + fallback em memória). A interface `Limitador` fica no consumidor.
- RF09 — Sweeper (`-mode=worker|all`, a cada 60 s):
  - Numa TX com `pg_try_advisory_xact_lock`, marca `expirado` até 500 holds vencidos por passada, em lote com `FOR UPDATE SKIP LOCKED`.
  - Passo exportado `VarrerExpirados(ctx)` para gatilho explícito nos testes.
- RF10 — Métricas por callbacks injetados (o domínio não conhece OTel), sem labels: `reserva_holds_criados_total`, `reserva_holds_indisponiveis_total`, `reserva_holds_expirados_total`, `reserva_sweeper_execucoes_total`.
- RF11 — Log `info` de trava, liberação e extensão com `sessao_id`, quantidade e resultado. Nunca token, hash ou IP.

## Requisitos não funcionais

- RNF01 — Fronteiras (ADR 0003): depguard `reserva-domain` strict (stdlib, echo, zap, uuid, `internal/outbox` para `WithTx`, `internal/reserva/db`). O driver (`pgconn`/`pgx`) aparece só em `internal/reserva/db/erros.go`.
- RNF02 — O relógio é injetado. `agora` vai como parâmetro das queries, e nenhum `now()` do banco decide a expiração (ADR 0008).
- RNF03 — Testes com PG e Redis reais (testcontainers) e as migrations reais 001/002/007/008/009.

## Regras de negócio

- RN01 — No máximo 1 hold vivo por (sessão, assento).
- RN02 — Vivo = `status='ativo' AND expires_at > agora`. Um vencido pode ser roubado por outro dono e recebe id novo.
- RN03 — TTL 10 min; 1 extensão de +10 min.
- RN04 — Máximo de 6 holds vivos por dono, somando todas as sessões.
- RN05 — Operação sobre hold alheio responde 404, nunca 403.

## Critérios de aceite

- [ ] CA01 — Migration 009 up→down→up (CI).
- [ ] CA02 — Unit: VO de código (formato), lote (1–6, repetição), `Estender` (única, vencido), `Vivo` na borda exata de `expires_at` (relógio fixo).
- [ ] CA03 — **Corrida canônica**: 20 donos, barreira, mesmo assento → 1 × 201 + 19 × 409, 10 rodadas; query de invariante (1 vivo por assento, dono = o do 201).
- [ ] CA04 — Roubo: hold vencido (relógio +11 min) é travado por outro dono com id novo; 20 donos disputando o vencido → exatamente 1 vence.
- [ ] CA05 — Lote tudo ou nada: B pede [F7, F8] com F8 de A → 409 com `["F8"]` e F7 livre. Lotes cruzados concorrentes [F1, F2] × [F2, F1] em 10 rodadas → só 201/409, sem 500, e nenhum hold órfão.
- [ ] CA06 — Teto: 7º assento → 409 `limite_holds`; 2 lotes paralelos de 4 do mesmo dono → um 201 e um 409; repetir assento próprio é idempotente.
- [ ] CA07 — Extensão: 1ª → 200 (+10 min), 2ª → 409; hold alheio/vencido → 404. Liberar: 204, depois 404; o assento volta a ficar livre para outro dono.
- [ ] CA08 — Segurança: sem `X-Requested-With` → 403; 31ª requisição do IP no minuto → 429; cookie emitido com HttpOnly/Secure/SameSite=Strict; resposta sem token/hash; `GET /holds` só do próprio dono.
- [ ] CA09 — Porta: sessão cancelada/iniciada/inexistente → 404; código fora do layout (vão ou além da grade) → 400.
- [ ] CA10 — Sweeper: gatilho explícito marca vencidos como `expirado` e não toca vivos; corrida sweeper × roubo do mesmo assento → sem erro, 1 vivo.
- [ ] CA11 — CI verde (lint, `-race`, `sqlc diff`, depguard).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| VOs, lote e aggregate com relógio fixo | unit | CA02 |
| Rotas reais + PG/Redis reais + porta real do `sessao` | integração | CA03–CA10 |

## Plano de implementação

1. Migration 009, `sqlc.yaml`, `queries.sql` e geração.
2. `dominio.go` (VOs, aggregate, lote), `carrinho.go`, `errors.go`, `db/erros.go`, `repositorio.go`.
3. `service.go` (trava, extensão, liberação, meus holds), `sweeper.go`, `handler.go`.
4. Porta no `sessao`; `main.go` (montagem, métricas, limitadores, sweeper); depguard.
5. Testes; lint; `-race`; gate.

**Skills de apoio (§4.4):** `postgres-patterns`, `golang-concurrency`.

## Arquivos que serão criados

- `migrations/009_holds.up.sql`, `migrations/009_holds.down.sql`
- `internal/reserva/queries.sql`, `internal/reserva/db/{db.go,models.go,queries.sql.go}` (gerados), `internal/reserva/db/erros.go`
- `internal/reserva/dominio.go`, `carrinho.go`, `errors.go`, `repositorio.go`, `service.go`, `sweeper.go`, `handler.go`
- `internal/reserva/dominio_test.go`, `internal/reserva/handler_test.go` (integração)
- `docs/tasks/0015-reserva-trava.md`, `docs/prd/0015-reserva-trava.md`

## Arquivos que serão modificados

- `sqlc.yaml`, `internal/sessao/service.go` (porta), `cmd/morfeu/main.go`, `.golangci.yml`
- `docs/tasks/README.md`, `plan.md`, `state.md`

Total: 25.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- Tabela nova com FK para `sessoes` (integridade declarada; o módulo não lê a tabela).
- O worker ganha mais uma goroutine periódica.
- Novas rotas públicas de escrita, protegidas por cookie + anti-CSRF + rate limit.

## Riscos

- Deadlock entre lotes → ordem global + retry em 40P01 + teste cruzado.
- Relógio da aplicação × banco → `agora` é sempre parâmetro, nunca `now()`.
- Flakiness da corrida no CI ARM64 → barreira + 10 rodadas + logs do teste só em falha.

## Estratégia de rollback

Reverter o merge; `migrate down 1` (009 down: `DROP TABLE holds`). Nenhuma tabela existente é alterada.
