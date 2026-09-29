# Task 0005 — Consumidor idempotente + DLQ (E0b, parte 2/2)

- **Data:** 2026-07-13
- **Status:** em andamento
- **Branch:** `feature/0005-consumidor-idempotente`
- **PRD:** docs/prd/0005-consumidor-idempotente.md (ativo)
- **Item do roadmap:** E0b — outbox + RabbitMQ + worker idempotente (parte 2/2; divisão registrada na abertura do PRD 0002, roles.md §6.3)

## Objetivo

Fechar o walking skeleton assíncrono: consumir `catalogo.filme_criado` da quorum queue com **dedup transacional** (`processed_messages` na mesma TX do efeito), aplicar a projeção de domínio, exercitar a DLQ com mensagem envenenada sem derrubar o processo, e incluir o RabbitMQ no readiness. Com o merge, o caminho `CLI → outbox → relay → broker → consumer → projeção` roda fim a fim — a fundação que a saga do checkout (E6) reutiliza.

## Escopo

Conforme refinamento §"Task E0b" (parte consumidora) + ADR 0007 + contrato de envelope do PRD 0002 (RF05):

- **Migration 003**: `processed_messages` (dedup, ownership excepcional da plataforma como `outbox_events` — RN03 do PRD 0002) + tabela da projeção do domínio consumidor.
- **Runtime do consumer em plataforma** (`internal/broker` ou pacote irmão): conexão/canal de consumo sobre o `broker.Client` existente, ack/nack, graceful shutdown por ctx (SIGTERM, timeout 30s, sem goroutine órfã); ativo só em `-mode=worker|all`.
- **Wrapper de dedup em plataforma**: mensagem processada = efeito de domínio + insert em `processed_messages` **na mesma TX**; `message_id` repetido → efeito não reaplica e mensagem é ack'ada.
- **Handler no domínio** (`internal/catalogo`): projeção do evento (efeito verificável no banco), sem importar `amqp091-go` (depguard existente).
- **DLQ exercitada**: payload malformado → nack sem requeue infinito (a topologia `x-delivery-limit=3` da 0002 encaminha à DLQ) — processo não cai, fila principal não trava.
- **Readiness**: health/readiness passa a incluir RabbitMQ sem quebrar o contrato da E0a.
- **Compose**: worker com `depends_on: condition: service_healthy` do RabbitMQ (healthcheck já existe da 0002).
- Testes de integração (testcontainers PG+RabbitMQ reais, `-race`, goleak): dedup por estado final (2× → efeito 1×), DLQ, shutdown sem perda/duplicação, migration 003 up→down→up, regressão E0a/0002.

## Fora de escopo

Replay de DLQ (E6); LISTEN/NOTIFY; limpeza de `outbox_events`/`processed_messages` (débito registrado); métricas custom (E0d — mas a fonte `outbox_pendentes` da 0002 permanece); SDK OTel completo (E0d); segunda queue/evento; shell SPA (0006 candidata).

## Arquivos esperados

Estimativa ~26 (≤ 30; superfície autoral ~16): migration 003 (2) · consumer runtime + dedup wrapper + queries + sqlc gerado (~7) · handler/projeção no catálogo + queries + gerado (~4) · `cmd/morfeu/main.go` (1) · `internal/health` readiness (1–2) · `docker-compose.yml` (1) · `sqlc.yaml` (1) · testes (~2) · docs de controle (task/README/prd/plan/state/lib — 6). `go.mod`/`go.sum` sem mudança esperada (deps já registradas na 0002).

## Dependências esperadas

Nenhuma nova: `amqp091-go`, `testcontainers` (+ módulo rabbitmq), `goleak`, `moby/moby/api` (test-only) já registradas no lib.md. Se o PRD identificar dep nova → registrar antes do import (§6.9).

## Critérios de aceite

- [ ] Mensagem consumida 2× (redelivery real) → efeito de domínio aplicado 1× e ack na 2ª (dedup por estado final, integração).
- [ ] Dedup e efeito na **mesma TX**: falha após o efeito e antes do commit → nem efeito nem `processed_messages` persistem; redelivery reaplica com sucesso.
- [ ] Payload malformado → mensagem termina na DLQ após `x-delivery-limit`, processo vivo, fila principal segue consumindo.
- [ ] Fim a fim: `criar-filme` (CLI) → relay publica → consumer projeta; efeito verificável no banco (walking skeleton fechado).
- [ ] Graceful shutdown (SIGTERM) durante consumo: sem perda nem duplicação de efeito; goleak sem vazamento.
- [ ] Readiness inclui RabbitMQ; contrato da E0a (`/health`, `GET /filmes`) intacto (regressão).
- [ ] Migration 003 up→down→up idempotente; `sqlc vet`/`diff` limpos (gates do CI).
- [ ] Suíte completa verde com `-race` no CI ARM64.

## Riscos

- Dedup fora da TX do efeito (efeito 2× ou perda) → wrapper único de plataforma força a composição, teste de falha-entre-efeito-e-commit.
- Redelivery infinito de mensagem envenenada → `x-delivery-limit=3` (topologia da 0002) + teste explícito de DLQ.
- Corrida no shutdown (mensagem em voo) → ctx + espera do handler em curso antes do close; teste dedicado.
- Estouro do limite de 30 arquivos → lista fechada acima; arquivo extra exige atualizar o PRD antes.

## Estimativa de impacto

Médio em código (novo runtime de consumo em plataforma + handler de domínio), baixo em banco (migration 003 aditiva com down completo), baixo em infra (compose aditivo), nenhum em usuários (sem contrato HTTP novo; readiness é aditivo).
