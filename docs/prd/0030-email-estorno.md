# PRD 0030 — E-mail de estorno (E7, T3)

- **Task:** docs/tasks/0030-email-estorno.md
- **Branch:** feature/0030-email-estorno
- **Data:** 2026-09-30
- **Status:** concluído

## Objetivo

Fechar o E7. Quando o estorno automático (0025) conclui, o cliente recebe um aviso: o valor foi devolvido e o ingresso não vale. Mesmo padrão do e-mail de confirmação: evento só com `pedido_id` pela outbox, fila própria, dedup, provedor da 0029. Fonte: `docs/refinamentos/E7-notificacao.md` §T3.

## Requisitos funcionais

- RF01 — `estornarUm` enfileira `pedido.estornado` (`{"pedido_id"}`) na **mesma TX** do CAS `estorno_pendente → estornado`. CAS perdido → rollback → nenhum evento duplicado.
- RF02 — Porta `pedido.DadosParaAvisoDeEstorno(id)` → e-mail, código, total.
  - Só atende pedido `estornado`.
  - Outro status, inclusive `estorno_pendente` → `ErrNaoNotificavel`.
  - Inexistente → `ErrPedidoNaoEncontrado`.
  - Reutiliza a query da porta de confirmação.
- RF03 — Topologia: quorum queue `notificacao.pedido_estornado` (routing key `pedido.estornado`) com DLX/DLQ próprias. A DLQ entra em `FilasReplay`, e com isso no `replay-dlq` e no gauge `morfeu_dlq_mensagens{fila}`.
- RF04 — `notificacao.AvisoDeEstorno`:
  - Assunto fixo "Seu estorno foi concluído — Morfeu".
  - HTML + texto com código e valor devolvido.
  - **Sem QR, sem link de ingresso, sem token.**
  - Chave de idempotência `estorno-{pedido_id}`.
- RF05 — O consumidor passa a ser tipado (`Config.Tipo`), e o do estorno registra `tipo=estorno` nas métricas `morfeu_email_*`. O estorno não mede o SLI da compra (sem latência). Dedup próprio: `notificacao.pedido_estornado`.
- RF06 — As séries de recusa e cota nascem em 0 também para `tipo=estorno`.

## Regras de negócio

- RN01 — O pedido pago tarde recebe só o aviso de estorno: o caminho tardio nunca publica `pedido.confirmado`, e a porta de confirmação recusa o que não está `pago`.

## Critérios de aceite

- [x] CA01 — Pago tarde → `estorno_pendente` → o aviso ainda é recusado. Estorno executado (2 rodadas) → exatamente 1 `pedido.estornado` e 0 `pedido.confirmado`.
- [x] CA02 — A porta devolve e-mail, código e total do estornado; inexistente → não encontrado.
- [x] CA03 — Aviso sem anexos, sem `<img`, sem `/i/`, com código e R$ 60,00; assunto e chave corretos.
- [x] CA04 — O aviso de pedido não estornado não envia; o consumidor de estorno registra `tipo=estorno`.
- [x] CA05 — CI verde (replay e gauge cobrem a DLQ nova pela lista fechada).

## Arquivos que serão criados

- `docs/tasks/0030-email-estorno.md`, `docs/prd/0030-email-estorno.md`

## Arquivos que serão modificados

- `internal/pedido/{tarefas.go, pivo.go, notificacao.go, tarefas_test.go}`
- `internal/broker/{client.go, replay.go}`
- `internal/notificacao/{consumidor.go, email.go, consumidor_test.go}`
- `cmd/morfeu/main.go`, `docs/ambiente-dev.md`
- `docs/tasks/README.md`, `docs/roadmap.md`, `plan.md`, `state.md`

Total: 17.

## Dependências utilizadas

Nenhuma nova.

## Estratégia de rollback

Reverter o merge. A fila nova fica órfã no broker, sem efeito.
