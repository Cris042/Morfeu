# ADRs — Architecture Decision Records

Decisões arquiteturais relevantes do projeto. Criadas **apenas** via skill `criar-adr`, com autorização do usuário (regras em `roles.md` §6.1). Template em `.claude/skills/criar-adr/template-adr.md`.

Convenção: `NNNN-titulo-kebab.md`, numeração sequencial a partir de `0001`. Status: proposto → aceito → substituído/obsoleto.

## Índice

| # | Título | Status | Data |
|---|--------|--------|------|
| [0001](0001-stack-backend-go-echo.md) | Stack backend: Go + Echo (monolito modular, binário único) | aceito | 2026-07-07 |
| [0002](0002-camada-de-dados-sqlc-pgx.md) | Camada de dados: sqlc + pgx/v5 | aceito | 2026-07-07 |
| [0003](0003-fronteiras-de-modulos-e-camadas.md) | Fronteiras de módulos e camadas do monolito | aceito | 2026-07-07 |
| [0004](0004-padroes-de-codigo-go.md) | Padrões de código Go (calisthenics adaptado, erros, TX, naming) | aceito | 2026-07-07 |
| [0005](0005-ddd-tatico-e-padroes-de-projeto.md) | DDD tático e padrões de projeto nos módulos críticos | aceito | 2026-07-07 |
| [0006](0006-estrategia-de-testes.md) | Estratégia de testes | aceito | 2026-07-07 |
| [0007](0007-mensageria-rabbitmq.md) | Mensageria: RabbitMQ (topologia, outbox + confirms + dedup, limites na A1) | aceito | 2026-07-11 |
| [0008](0008-trava-de-assento.md) | Trava de assento: PG com índice único parcial, expiração lazy e sweeper | aceito | 2026-09-29 |
