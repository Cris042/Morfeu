# Task 0008 — Identidade: registro/login + JWT + RBAC + seed do operador (E1, T1)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0008-identidade-registro-login` (criada sobre `chore/0007-stack-observabilidade`, PR #31 aguardando merge — rebase/base `main` após o merge)
- **PRD:** docs/prd/0008-identidade-registro-login.md — criar antes de implementar (just-in-time, §6.2.5)
- **Item do roadmap:** E1 — Identidade (task 1/2). Refinamento: `docs/refinamentos/E1-identidade.md` §"T1".

## Objetivo

Primeiro módulo com usuário: cadastro de cliente, login com Argon2id e access token JWT, autorização por papel (cliente|operador) aplicável por qualquer módulo sem importar `identidade`, e criação do operador só por CLI. Base de autenticação que E2 (backoffice) e E8 (SPA) consomem.

## Escopo

Exigências do refinamento E1 §T1: `internal/identidade` (tabela `usuario`, registro, login, anti-enumeração, rate limit com fallback em memória, semáforo de hashing), `internal/autenticacao` (JWT HS256 + `kid`, claims sem PII, middleware Echo, exigência de papel fail-closed), subcomando `seed-operador`, métricas `auth_*`, regras depguard, testes (matriz RBAC, contrato anti-enumeração, rate limit com relógio injetável, senha fora dos logs).

## Fora de escopo

Refresh token, logout, deleção de conta (T2 — task 0009); 2FA/CAPTCHA (Fase 2); SPA (E5/E8); rotas do backoffice (E2 aplicam o middleware).

## Arquivos esperados

~22–25 (estimativa do backend-dev no refinamento): migration 005 (2) · `internal/identidade` (handler, service, errors, ratelimit, queries.sql, db gerado ~3, testes ~2) · `internal/autenticacao` (jwt, middleware, testes) · `cmd/morfeu/main.go` · `internal/config` · `.golangci.yml` · `sqlc.yaml` · `internal/telemetria` (allowlist de labels) · `.env*.example` · `go.mod`/`go.sum` · `lib.md` · controle (task/README/prd/plan/state).

## Dependências esperadas

`golang-jwt/jwt/v5` ≥ 5.2.2 (nova) e `golang.org/x/crypto` (argon2, indireta → direta). Registrar no `lib.md` antes do import.

## Critérios de aceite

- [ ] Registro cria sempre `cliente`; e-mail duplicado e senha fora de 8–128 rejeitados; papel do payload não é aceito.
- [ ] Login devolve JWT HS256 (10 min, claims só `sub`/`papel`/`iat`/`exp`, `kid`); falhas com resposta idêntica para e-mail inexistente e senha errada.
- [ ] Matriz RBAC: {sem token, cliente, operador, expirado, adulterado, `alg` trocado} → 401/403/200 exatos.
- [ ] `seed-operador` idempotente; senha aleatória exibida 1×; nenhuma rota HTTP cria operador.
- [ ] Rate limit por conta e IP com Redis e fallback em memória quando o Redis falha; relógio injetável nos testes.
- [ ] Semáforo de hashing → 429 quando saturado.
- [ ] Senha nunca aparece em log; métricas `auth_*` sem PII.
- [ ] Depguard: nenhum módulo importa `identidade`; CI verde.

## Riscos

- Complexidade de segurança concentrada → testes de contrato e integração (PG + Redis reais).
- Estouro de arquivos → lista fechada no PRD.

## Estimativa de impacto

Alto em código (2 pacotes novos, primeira rota com escrita pública), baixo em banco (tabela nova), nenhum em infra (Redis já existe), nenhum em usuários ainda (sem SPA).
