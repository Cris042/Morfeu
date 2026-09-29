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
| 0007 | [Stack de observabilidade](./0007-stack-observabilidade.md) | E0d (2/2) | em PR #31 (auditoria APROVADA 2026-09-29, CI verde; merge aguarda autorização) | `chore/0007-stack-observabilidade` | [0007](../prd/0007-stack-observabilidade.md) |
| 0008 | [Identidade: registro/login + JWT + RBAC + seed](./0008-identidade-registro-login.md) | E1 (T1) | em andamento (aberta 2026-09-29) | `feature/0008-identidade-registro-login` | 0008 (PRD a criar) |
