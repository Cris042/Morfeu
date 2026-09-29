# PRD 0005 — Consumidor idempotente + DLQ (E0b, parte 2/2)

- **Task:** docs/tasks/0005-consumidor-idempotente.md
- **Branch:** feature/0005-consumidor-idempotente
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Fechar o walking skeleton assíncrono do E0b: consumir `catalogo.filme_criado` da quorum queue com **dedup transacional** (`processed_messages` na mesma TX do efeito), aplicar uma projeção no domínio `catalogo`, exercitar a DLQ (mensagem envenenada e limite de redelivery) sem derrubar o processo e expor o RabbitMQ no health. Com o merge, `CLI criar-filme → outbox → relay → broker → consumer → projeção` roda fim a fim — fundação reutilizada pela saga do E6 (ADR 0007).

Fontes: refinamento E0 §"Task E0b" (parte consumidora), ADR 0007, contrato de envelope do PRD 0002 (RF05/RF07), ADRs 0002/0003/0006.

## Escopo

- Migration 003 `processed_messages` (plataforma) e migration 004 `catalogo_filmes_projetados` (domínio catalogo) — duas migrations para que cada bloco do `sqlc.yaml` só enxergue as tabelas que lhe pertencem (mesmo racional do bloco outbox da 0002).
- Runtime de consumo em `internal/broker` (única fronteira do `amqp091-go`): canal dedicado, `Qos`, ack manual, resubscrição após reconexão, graceful shutdown por ctx.
- Wrapper de dedup em `internal/outbox` (plataforma que já detém `WithTx`/pgx): registro em `processed_messages` + efeito de domínio na mesma TX.
- Projeção no domínio `internal/catalogo` (sem AMQP, sem pgx — só `outbox.Tx` + sqlc gerado).
- Wiring em `cmd/morfeu/main.go` (consumer só em `-mode=worker|all`, junto do relay).
- Health: campo `rabbitmq` aditivo quando o processo tem broker.

## Fora de escopo

Replay de DLQ (E6); LISTEN/NOTIFY; limpeza de `outbox_events`/`processed_messages` (débito registrado); métricas custom e SDK OTel (E0d — traceparent recebido só é logado); segunda queue/evento; shell SPA (0006 candidata). **Serviço worker no compose**: o `docker-compose.yml` hoje só tem infra (postgres/redis/rabbitmq) — não existe serviço de app para receber `depends_on: service_healthy`; a exigência do refinamento passa para a E0c-CD (deploy), onde o serviço de app nasce. Nenhuma mudança no compose nesta task.

## Requisitos funcionais

- RF01 — `broker.Client.Consumir(ctx, fila string, h Handler) error`: abre canal próprio na conexão atual, `Qos(prefetch=1)`, `Consume` com `autoAck=false`, entrega cada mensagem a `h` como `broker.Entrega` (`MessageID`, `Type`, `AggregateID`, `Traceparent`, `Body`, `Redelivered`) — nunca `amqp.Delivery` cru. Bloqueia até ctx cancelado.
- RF02 — Resultado do handler: `nil` → `Ack`; erro marcado `broker.ErrPermanente` (via `errors.Is`) → `Nack(requeue=false)` (dead-letter imediato para a DLQ); outro erro → `Nack(requeue=true)` (redelivery; a quorum queue com `x-delivery-limit=3` dead-lettera após o limite).
- RF03 — Reconexão: se o canal de entregas fecha (queda do broker), `Consumir` espera o `Client` reconectar (backoff existente, RF06 da 0002) e reassina a fila; sem crash, sem loop quente.
- RF04 — Graceful shutdown: ctx cancelado → não pega nova entrega; a entrega em processamento termina (handler roda com `context.WithoutCancel` + timeout 30s) e é ack'ada/nack'ada; então `Cancel` do consumer e close do canal. Entregas não ack'adas voltam à fila pelo broker (sem perda).
- RF05 — `outbox.ProcessarUmaVez(ctx, pool, consumidor string, msg Mensagem, efeito func(ctx, Tx, Mensagem) error) (duplicada bool, err error)`: dentro de `WithTx`, `INSERT INTO processed_messages (message_id, consumidor) ... ON CONFLICT DO NOTHING RETURNING`; sem linha → duplicada, efeito não roda, commit vazio, retorna `(true, nil)` (→ ack); com linha → roda `efeito(tx)` na mesma TX; erro do efeito → rollback de ambos.
- RF06 — `outbox.ErrPermanente` (reexporta/embrulha `broker.ErrPermanente`) para o domínio marcar erro não-retentável sem importar `broker`.
- RF07 — Projeção `catalogo.ProjetorFilmeCriado.Aplicar(ctx, tx, msg)`: decodifica o payload (`filmeCriadoPayload` da 0002); JSON inválido/campos obrigatórios ausentes → erro `ErrPermanente`; válido → upsert em `catalogo_filmes_projetados (film_id, titulo, ano, aplicacoes, projetado_em)` com `aplicacoes = aplicacoes + 1` no conflito — a coluna `aplicacoes` é a prova observável de idempotência (dedup correto ⇒ sempre 1). Log `info` só com `event_type`, `aggregate_id`, `message_id`.
- RF08 — Wiring: em `-mode=worker|all`, após o relay, sobe goroutine `Consumir(QueueFilmeCriado, ...)` registrada no mesmo WaitGroup do shutdown; `-mode=api` nunca consome.
- RF09 — Health: `HealthResponse` ganha `RabbitMQ string json:"rabbitmq,omitempty"` preenchido (`ok`/`error`, via `broker.Client.Conectado()`) só quando o processo tem broker; em `-mode=api` o campo é omitido — JSON da E0a idêntico; status HTTP segue 200.

## Requisitos não funcionais

- RNF01 — Fronteiras (ADR 0003): `amqp091-go` só em `internal/broker` (depguard existente); `catalogo` não importa `broker`, `pgx` nem `amqp091-go`.
- RNF02 — sqlc (ADR 0002): SQL novo só via `queries.sql` + `sqlc generate`; `sqlc vet`/`diff` limpos.
- RNF03 — Segurança: logs do consumer em `info` sem payload; mensagem com `message_id` vazio → `ErrPermanente` (sem chave de dedup não há idempotência).
- RNF04 — Concorrência: sem goroutine órfã (goleak no `TestMain` do pacote outbox já existente); `-race` limpo.
- RNF05 — Testes (ADR 0006): testcontainers reais PG+RabbitMQ, filas nomeadas por teste quando o cenário exige isolamento, sem sleep fixo (poll com deadline).

## Regras de negócio

- RN01 — Uma mensagem (por `message_id` + `consumidor`) produz efeito no máximo uma vez; at-least-once no transporte, exactly-once no efeito.
- RN02 — Mensagem inválida nunca bloqueia a fila principal: vai à DLQ e o consumo segue.
- RN03 — `processed_messages` é ownership excepcional da plataforma (como `outbox_events`, RN03 do PRD 0002); `catalogo_filmes_projetados` pertence ao `catalogo`.

## Critérios de aceite

- [ ] CA01 — Mesma mensagem publicada 2× (mesmo `message_id`) → `aplicacoes = 1` e ambas ack'adas (fila vazia).
- [ ] CA02 — Efeito que falha após o upsert (injeção no teste) → nem projeção nem `processed_messages` persistem; redelivery com efeito saudável aplica 1×.
- [ ] CA03 — Payload malformado → DLQ (`catalogo.filme_criado.dlq`) recebe a mensagem; mensagem válida publicada depois é projetada (fila principal viva).
- [ ] CA04 — Erro transitório persistente → após `x-delivery-limit=3` a mensagem vai à DLQ (limite nativo exercitado).
- [ ] CA05 — Fim a fim: `CreateFilm` (service real) → relay → consumer → linha em `catalogo_filmes_projetados`.
- [ ] CA06 — Shutdown com mensagem em processamento: efeito concluído e ack'ado exatamente 1×; `Consumir` retorna; goleak limpo.
- [ ] CA07 — Health em processo com broker traz `"rabbitmq":"ok"`; sem broker o JSON é o da E0a; `GET /filmes` e asserts pré-existentes intactos.
- [ ] CA08 — Migrations 003/004 up→down→up (job `migrations` do CI); `sqlc vet`/`diff` limpos.
- [ ] CA09 — Suíte completa `-race` verde no CI ARM64.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Publica envelope válido 2× com mesmo `message_id` → `aplicacoes=1`, fila vazia | integração (PG+RabbitMQ) | CA01, RF05, RN01 |
| Efeito que injeta erro após upsert → rollback total; 2ª entrega aplica | integração | CA02 |
| Body `{"x":` → DLQ recebe; mensagem válida seguinte projetada | integração | CA03, RF02, RN02 |
| Handler sempre erro transitório → DLQ após 3 entregas (contagem de chamadas ≥ 3) | integração (fila própria do teste com mesmos args) | CA04 |
| `CreateFilm` + relay + consumer rodando → projeção aparece (poll com deadline) | integração e2e | CA05 |
| Cancelar ctx com handler bloqueado em curso → handler termina, ack, `Consumir` retorna, efeito 1× | integração | CA06, RF04 |
| `message_id` vazio → DLQ | integração | RNF03 |
| Health com/sem broker (JSON) | integração (`internal/health`) | CA07, RF09 |
| Regressão: suíte E0a/0002 inalterada | integração | CA07 |

## Plano de implementação

1. Migrations 003/004 (`criar-migration`) + blocos `sqlc.yaml` + queries (`outbox/queries.sql`: `RegistrarProcessada`; `catalogo/queries.sql`: `UpsertFilmeProjetado`, `BuscarFilmeProjetado`) + `sqlc generate`.
2. `internal/broker/consumer.go` (RF01–RF04) + `Conectado()`.
3. `internal/outbox/dedup.go` (RF05/RF06).
4. `internal/catalogo/projecao.go` (RF07).
5. `cmd/morfeu/main.go` (RF08) + `internal/health` (RF09).
6. Testes (`internal/outbox/consumidor_integration_test.go`, `internal/health/health_test.go`); schema do helper de teste passa a aplicar 003/004.
7. Gate: lint/vet/`-race` local via container → gitleaks → push → PR → CI → passe de julgamento → merge.

**Skills de apoio (§4.4):** `golang-concurrency`, `golang-database`.

## Arquivos que serão criados

- `migrations/003_processed_messages.up.sql` / `.down.sql` — tabela de dedup (PK `message_id, consumidor`).
- `migrations/004_catalogo_filmes_projetados.up.sql` / `.down.sql` — projeção do catálogo.
- `internal/broker/consumer.go` — runtime de consumo.
- `internal/outbox/dedup.go` — wrapper `ProcessarUmaVez`.
- `internal/catalogo/projecao.go` — projetor do evento.
- `internal/outbox/consumidor_integration_test.go` — CA01–CA06.
- `docs/prd/0005-consumidor-idempotente.md` — este PRD.

## Arquivos que serão modificados

- `sqlc.yaml` — schema 003 no bloco outbox; 004 no bloco catalogo.
- `internal/outbox/queries.sql`, `internal/outbox/db/queries.sql.go`, `internal/outbox/db/models.go` — query de dedup (gerado).
- `internal/catalogo/queries.sql`, `internal/catalogo/db/queries.sql.go`, `internal/catalogo/db/models.go`, `internal/catalogo/db/querier.go` — projeção (gerado).
- `internal/outbox/relay_integration_test.go` — helper de schema aplica 003/004 (sem mudar asserts).
- `internal/broker/client.go` — `Conectado()` e acesso à conexão atual para o consumer.
- `cmd/morfeu/main.go` — wiring do consumer + health com broker.
- `internal/health/health.go`, `internal/health/health_test.go` — campo `rabbitmq`.
- `docs/tasks/0005-consumidor-idempotente.md`, `docs/tasks/README.md`, `plan.md`, `state.md` — controle.

Total previsto: 26 (autorais ~15; gerados 5; controle 5).

## Dependências utilizadas

Nenhuma nova: `amqp091-go`, `pgx/v5`, `uuid`, `zap`, `testcontainers-go` (+ rabbitmq), `goleak` — todas já no `lib.md`.

## Impactos técnicos

- `broker` ganha API de consumo; `outbox` passa a ser o pacote de plataforma de mensageria transacional (produção + dedup de consumo).
- Banco: 2 tabelas aditivas. Contratos HTTP: só campo aditivo opcional no `/health`.
- Worker agora consome além de publicar; `-mode=api` inalterado.

## Riscos

- Dedup fora da TX → `ProcessarUmaVez` é o único caminho e recebe o efeito como função (composição forçada); CA02 prova.
- Consumer não reassina após reconexão → RF03 com teste manual de stop/start coberto pelo padrão da 0002 (se o custo do teste passar do razoável, fica evidenciado por log e registrado).
- Nack com requeue em loop quente → prefetch 1 + `x-delivery-limit=3` limita a 3 tentativas.
- Handler preso no shutdown → timeout de 30s no ctx de processamento.
- Estouro de 30 arquivos → lista fechada acima; arquivo extra exige atualizar o PRD antes.

## Estratégia de rollback

Reverter o merge; `migrate down 2` executa `004…down.sql` (`DROP TABLE catalogo_filmes_projetados`) e `003…down.sql` (`DROP TABLE processed_messages`). Nenhuma tabela existente é alterada. Mensagens já na fila permanecem (sem consumer, acumulam — o relay segue publicando normalmente).
