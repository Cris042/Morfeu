# Task 0008 — Autenticação (plataforma): JWT, middleware de papel, limitador de tentativas (E1, T1 parte 1/2)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0008-autenticacao-plataforma` (criada sobre `chore/0007-stack-observabilidade`; PR #31 aguarda merge — base `main` após o merge)
- **PRD:** docs/prd/0008-autenticacao-plataforma.md — criar antes de implementar (just-in-time, §6.2.5)
- **Item do roadmap:** E1 — Identidade. Refinamento: `docs/refinamentos/E1-identidade.md` §"T1".

## Divisão da T1 (roles.md §6.3)

A lista fechada de arquivos da T1 do refinamento soma ~34 (> 30) com o registro do refinamento e os docs de controle — a estimativa do refinamento (18–25) não contou gerados/controle. Divisão pela fronteira natural, sem mudança de escopo do épico:

- **0008 (esta) — plataforma `internal/autenticacao`**: emissão/validação de JWT, claims, middleware de exigência de papel (fail-closed), limitador de tentativas (Redis + fallback em memória, relógio injetável), métricas `auth_*`. Biblioteca pronta e testada, ainda sem rota.
- **0009 — módulo `identidade`**: tabela `usuario`, registro, login (anti-enumeração, semáforo de hashing), `seed-operador`, `GET /auth/eu`, wiring/config (`JWT_SEGREDO`), depguard de `identidade`.
- **0010 — T2**: refresh rotativo + detecção de reuso + pseudonimização.

## Objetivo

Entregar a peça de plataforma que todo módulo usa para autenticar/autorizar sem importar `identidade` (ADR 0003): JWT HS256 com `kid` e allowlist de algoritmo, claims sem PII, middleware Echo fail-closed e o limitador de tentativas exigido pelo refinamento.

## Escopo

`internal/autenticacao`: `Emissor` (emite/valida, TTL 10 min, relógio injetável), `Claims` (`sub`, `papel`, `iat`, `exp`), `Exigir(emissor, papeis...)` (sem token/ inválido → 401; papel fora → 403; lista vazia → nega), helpers de contexto (`UsuarioID`, `Papel`), `Limitador` (Redis INCR+TTL; falha do Redis → contador em memória; relógio injetável), instrumentos `auth_*`; allowlist de labels da telemetria (`resultado`, `escopo`); regra depguard da plataforma; deps no `lib.md`.

## Fora de escopo

Qualquer rota, tabela ou wiring em `main.go` (0009); refresh/logout (0010).

## Arquivos esperados

~16: `internal/autenticacao/{jwt,middleware,limitador,metricas}.go` + 2 testes · `internal/telemetria/telemetria.go` · `.golangci.yml` · `go.mod`/`go.sum` · `lib.md` · refinamento E1 (2, já commitados) · controle (task/README/prd/plan/state).

## Dependências esperadas

`github.com/golang-jwt/jwt/v5` v5.3.1 (nova). `golang.org/x/crypto` entra só na 0009.

## Critérios de aceite

- [ ] JWT HS256 com `kid`; claims só `sub`/`papel`/`iat`/`exp`; TTL 10 min; segredo < 32 bytes recusado.
- [ ] Validação rejeita `alg` fora da allowlist (inclusive `none`), assinatura adulterada, expirado, `kid` desconhecido, sem `exp`.
- [ ] Matriz do middleware: {sem token, malformado, expirado, adulterado, cliente, operador} × {exige cliente, exige operador, exige ambos, lista vazia} → 401/403/200 exatos.
- [ ] Limitador: bloqueia na N+1ª falha dentro da janela e libera após a janela (relógio injetável, sem sleep); Redis real (testcontainers) e fallback em memória com Redis indisponível.
- [ ] Métricas `auth_*` com labels só da allowlist.
- [ ] CI verde.

## Riscos

- Biblioteca sem consumidor até a 0009 → consumida na próxima task; testes cobrem o contrato.

## Estimativa de impacto

Médio em código (pacote de plataforma novo), nenhum em banco/infra/usuários.
