# Tasks

Tasks do projeto, criadas via skill `criar-task` a partir do roadmap (regras em `roles.md` §6.3). Template em `.claude/skills/criar-task/template-task.md`.

Convenção: `NNNN-titulo-kebab.md`, numeração sequencial a partir de `0001`. Cada task = 1 branch + 1 PRD + máx. 30 arquivos alterados.

## Índice

| # | Título | Roadmap | Status | Branch | PRD |
|---|--------|---------|--------|--------|-----|
| 0001 | [App Skeleton Go](./0001-app-skeleton-go.md) | E0a | concluída (merge `6916ab6`; fix de build PR #3 `fa2a136`) | `feature/0001-app-skeleton-go` | [0001](../prd/0001-app-skeleton-go.md) |
| 0002 | [Outbox + RabbitMQ: lado produtor](./0002-outbox-rabbitmq.md) | E0b (1/2) | concluída (auditoria APROVADA 2026-07-13; merge `a1d1924`, PR #19) | `feature/0002-outbox-rabbitmq` | [0002](../prd/0002-outbox-rabbitmq.md) |
| 0003 | [Conformidade package-by-domain](./0003-conformidade-package-by-domain.md) | E0 (pré-E0b) | concluída (auditoria APROVADA; merge `a391cdb`, PR #4) | `refactor/0003-conformidade-package-by-domain` | [0003](../prd/0003-conformidade-package-by-domain.md) |
| 0004 | [Pipeline de CI + build ARM64](./0004-pipeline-ci-arm64.md) | E0c-CI | concluída (auditoria + passe delta APROVADOS; 6/6 provas bloqueadas; merge `7585522`, PR #6) | `chore/0004-pipeline-ci-arm64` | [0004](../prd/0004-pipeline-ci-arm64.md) |
| 0005 | [Consumidor idempotente + DLQ](./0005-consumidor-idempotente.md) | E0b (2/2) | concluída (auditoria APROVADA 2026-09-29; merge `c35957d`, PR #29) | `feature/0005-consumidor-idempotente` | [0005](../prd/0005-consumidor-idempotente.md) |
| 0006 | [Instrumentação da app: OTel + /metrics + logs](./0006-instrumentacao-otel-metricas.md) | E0d (1/2) | concluída (auditoria APROVADA 2026-09-29; merge `02831dc`, PR #30) | `feature/0006-instrumentacao-otel-metricas` | [0006](../prd/0006-instrumentacao-otel-metricas.md) |
| 0007 | [Stack de observabilidade](./0007-stack-observabilidade.md) | E0d (2/2) | concluída (auditoria APROVADA; merge `757851f`, PR #31) | `chore/0007-stack-observabilidade` | [0007](../prd/0007-stack-observabilidade.md) |
| 0008 | [Autenticação (plataforma): JWT, middleware de papel, limitador](./0008-autenticacao-plataforma.md) | E1 (T1 1/2) | concluída (auditoria APROVADA; merge `60cfc35`, PR #33 — reaberto do #32) | `feature/0008-autenticacao-plataforma` | [0008](../prd/0008-autenticacao-plataforma.md) |
| 0009 | [Identidade: usuário, registro, login, seed](./0009-identidade-registro-login.md) | E1 (T1 2/2) | concluída (auditoria APROVADA; merge `3ef3188`, PR #34) | `feature/0009-identidade-registro-login` | [0009](../prd/0009-identidade-registro-login.md) |
| 0010 | [Refresh rotativo + pseudonimização](./0010-refresh-pseudonimizacao.md) | E1 (T2) | concluída (auditoria APROVADA; merge `5d2ca7f`, PR #35) | `feature/0010-refresh-pseudonimizacao` | [0010](../prd/0010-refresh-pseudonimizacao.md) |
| 0011 | [Catálogo: migração `films`→`filmes` + CRUD do operador](./0011-catalogo-filmes-crud.md) | E2 (T1) | concluída (auditoria APROVADA; merge `dfe0d01`, PR #36) | `feature/0011-catalogo-filmes-crud` | [0011](../prd/0011-catalogo-filmes-crud.md) |
| 0012 | [Importação de filmes do TMDB](./0012-importacao-tmdb.md) | E2 (T2) | concluída (auditoria APROVADA; merge `1514361`, PR #37) | `feature/0012-importacao-tmdb` | [0012](../prd/0012-importacao-tmdb.md) |
| 0013 | [Sessões e salas: escrita e não-conflito](./0013-sessoes-salas-escrita.md) | E3 (T1) | concluída (auditoria APROVADA; merge `c5478a2`, PR #38) | `feature/0013-sessoes-salas-escrita` | [0013](../prd/0013-sessoes-salas-escrita.md) |
| 0014 | [Sessões: leitura pública, mapa e cache](./0014-sessoes-leitura-publica.md) | E3 (T2) | concluída (auditoria APROVADA; merge `fea38b1`, PR #39) | `feature/0014-sessoes-leitura-publica` | [0014](../prd/0014-sessoes-leitura-publica.md) |
| 0015 | [Reserva: trava de assento + sweeper](./0015-reserva-trava.md) | E4 (T1) | concluída (auditoria APROVADA; merge `36613c3`, PR #41) | `feature/0015-reserva-trava` | [0015](../prd/0015-reserva-trava.md) |
| 0016 | [Reserva: ocupação pública, cache e alertas](./0016-reserva-ocupacao.md) | E4 (T2) | concluída (auditoria APROVADA; merge `b9ad75a`, PR #42) | `feature/0016-reserva-ocupacao` | [0016](../prd/0016-reserva-ocupacao.md) |
| 0017 | [SPA: fundação (shell, tokens, cliente /api, CI do front)](./0017-spa-fundacao.md) | E5 (T1) | concluída (auditoria APROVADA; merge `cce783c`, PR #45) | `feature/0017-spa-fundacao` | [0017](../prd/0017-spa-fundacao.md) |
| 0018 | [SPA: cartaz e sessões do filme](./0018-spa-cartaz.md) | E5 (T2) | concluída (auditoria APROVADA; merge `849064b`, PR #46) | `feature/0018-spa-cartaz` | [0018](../prd/0018-spa-cartaz.md) |
| 0019 | [SPA: mapa de assentos isolado](./0019-spa-mapa.md) | E5 (T3) | concluída (auditoria APROVADA; merge `0117c41`, PR #47) | `feature/0019-spa-mapa` | [0019](../prd/0019-spa-mapa.md) |
| 0020 | [SPA: mapa integrado à trava](./0020-spa-reserva.md) | E5 (T4a) | concluída (auditoria APROVADA; merge `f355cd8`, PR #48) | `feature/0020-spa-reserva` | [0020](../prd/0020-spa-reserva.md) |
| 0021 | [E2E do M3: dois navegadores disputam o mesmo assento](./0021-e2e-m3.md) | E5 (T4b) | concluída (merge `38aa433`, PR #49) | `feature/0021-e2e-m3` | [0021](../prd/0021-e2e-m3.md) |
| 0022 | [Reserva: holds vendidos ocupam o assento + portas transacionais do pedido](./0022-reserva-portas-pedido.md) | E6 (T1) | concluída (auditoria APROVADA; PR #51) | `feature/0022-reserva-portas-pedido` | [0022](../prd/0022-reserva-portas-pedido.md) |
| 0023 | [Pedido: aggregate, máquina de estados e criação (gateway fake)](./0023-pedido-criacao.md) | E6 (T2) | concluída (auditoria APROVADA; PR #52) | `feature/0023-pedido-criacao` | [0023](../prd/0023-pedido-criacao.md) |
| 0024 | [Stripe + webhook assinado + pivô do checkout](./0024-stripe-webhook-pivo.md) | E6 (T3) | concluída (auditoria APROVADA; PR #53) | `feature/0024-stripe-webhook-pivo` | [0024](../prd/0024-stripe-webhook-pivo.md) |
| 0025 | [Estorno + reconciliação no worker](./0025-estorno-reconciliacao.md) | E6 (T4) | concluída (auditoria APROVADA; PR #54) | `feature/0025-estorno-reconciliacao` | [0025](../prd/0025-estorno-reconciliacao.md) |
| 0026 | [Observabilidade da saga + consumidor de notificação + integração ponta a ponta](./0026-observabilidade-saga.md) | E6 (T5) | concluída (auditoria APROVADA após 1 rodada de correção; PR #55) | `feature/0026-observabilidade-saga` | [0026](../prd/0026-observabilidade-saga.md) |
| 0027 | [Replay da DLQ + hardening do worker](./0027-replay-dlq-hardening.md) | E6 (T6) | concluída (auditoria APROVADA; PR #56) | `feature/0027-replay-dlq-hardening` | [0027](../prd/0027-replay-dlq-hardening.md) |
| 0028 | [Notificação: núcleo do e-mail de confirmação com QR](./0028-notificacao-nucleo.md) | E7 (T1) | concluída (auditoria APROVADA; PR #58) | `feature/0028-notificacao-nucleo` | [0028](../prd/0028-notificacao-nucleo.md) |
| 0029 | [Provedor Resend, config, métricas e alertas de e-mail](./0029-provedor-resend.md) | E7 (T2) | concluída (auditoria APROVADA; PR #59) | `feature/0029-provedor-resend` | [0029](../prd/0029-provedor-resend.md) |
| 0030 | [E-mail de estorno](./0030-email-estorno.md) | E7 (T3) | concluída (auditoria APROVADA; PR #60) | `feature/0030-email-estorno` | [0030](../prd/0030-email-estorno.md) |
| 0031 | [Backend do checkout e da conta](./0031-backend-checkout-conta.md) | E8 (T1) | concluída (auditoria APROVADA; PR #62) | `feature/0031-backend-checkout-conta` | [0031](../prd/0031-backend-checkout-conta.md) |
| 0032 | [SPA: sessão, login/cadastro e "Meus pedidos"](./0032-spa-sessao-conta.md) | E8 (T2) | concluída (auditoria APROVADA; PR #63) | `feature/0032-spa-sessao-conta` | [0032](../prd/0032-spa-sessao-conta.md) |
| 0033 | [SPA: checkout e pagamento](./0033-spa-checkout.md) | E8 (T3) | concluída (auditoria APROVADA; PR #64) | `feature/0033-spa-checkout` | [0033](../prd/0033-spa-checkout.md) |
| 0034 | [Backend: consulta de convidado e página do ingresso](./0034-consulta-ingresso.md) | E8 (T4) | concluída (auditoria APROVADA; PR #65) | `feature/0034-consulta-ingresso` | [0034](../prd/0034-consulta-ingresso.md) |
| 0035 | [SPA: consulta e ingresso + E2E do M4](./0035-spa-consulta-m4.md) | E8 (T5) | concluída (auditoria APROVADA; PR #66) | `feature/0035-spa-consulta-m4` | [0035](../prd/0035-spa-consulta-m4.md) |
| 0036 | [Backend: cancelamento pelo cliente e de sessão com vendidos](./0036-cancelamento.md) | E9 (T1) | concluída (auditoria APROVADA; PR #68) | `feature/0036-cancelamento` | [0036](../prd/0036-cancelamento.md) |
| 0037 | [Trilha de auditoria + consulta e cancelamento de pedido pelo operador](./0037-auditoria-operador.md) | E9 (T2) | concluída (auditoria APROVADA; PR #69) | `feature/0037-auditoria-operador` | [0037](../prd/0037-auditoria-operador.md) |
