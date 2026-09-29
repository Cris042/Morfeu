# Task 0009 — Identidade: usuário, registro, login, seed do operador (E1, T1 parte 2/2)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0009-identidade-registro-login` (empilhada sobre a 0008 → 0007; PRs #32/#31 aguardam autorização de merge)
- **PRD:** docs/prd/0009-identidade-registro-login.md
- **Item do roadmap:** E1 — Identidade. Refinamento: `docs/refinamentos/E1-identidade.md` §T1 (parte de módulo; a plataforma veio na 0008).

## Objetivo

Módulo `internal/identidade` (transaction script, ADR 0005): tabela `usuario`, registro de cliente, login com Argon2id e anti-enumeração, rate limit por conta/IP (limitador da 0008), semáforo de hashing, `GET /auth/eu`, criação do operador só por CLI, wiring e configuração do segredo JWT.

## Escopo

Exigências do refinamento §T1 não cobertas pela 0008: migration 005, rotas `POST /auth/registro`, `POST /auth/login`, `GET /auth/eu`, `seed-operador`, Argon2id configurável, anti-enumeração por contrato, semáforo, métricas de login, senha fora dos logs, depguard de `identidade`, `JWT_SEGREDO` no boot (api/all), smoke do CI com o segredo.

## Fora de escopo

Refresh/logout/deleção (0010); rotas de backoffice (E2); SPA (E5/E8).

## Arquivos esperados

~29 (≤ 30): migration (2) · `internal/identidade` (queries, db ×3, service, senha, handler, errors, 2 testes) · `cmd/morfeu/main.go` · `internal/config` (+ teste) · `.golangci.yml` · `sqlc.yaml` · `.github/workflows/ci.yml` · `.env.example`/`.env.docker-compose.example` · `go.mod`/`go.sum` · `lib.md` · controle (5).

## Dependências esperadas

`golang.org/x/crypto` (argon2) promovida a direta (v0.55.0 — última compatível com Go 1.25; advisories abertos são só em `ssh`/`openpgp`).

## Critérios de aceite

- [ ] Registro sempre `cliente`; e-mail duplicado → 409; entrada inválida → 400; papel do payload ignorado.
- [ ] Login: JWT válido no sucesso; falha com resposta idêntica para e-mail inexistente e senha errada; hash executado nos dois ramos.
- [ ] 6ª falha por conta (ou 21ª por IP) → 429; semáforo saturado → 429.
- [ ] `GET /auth/eu` com matriz {sem token, cliente, operador}.
- [ ] `seed-operador` idempotente, senha aleatória exibida 1×; nenhuma rota cria operador.
- [ ] Senha nunca em log (inclusive JSON malformado).
- [ ] Boot em api/all falha sem `JWT_SEGREDO` válido; CI verde.

## Riscos

- Teto de 30 arquivos → lista fechada no PRD.
- Timing: anti-enumeração provada por contrato + caminho de código, não por cronômetro (refinamento).

## Estimativa de impacto

Alto em código (primeira rota pública com escrita), baixo em banco (tabela nova), baixo em infra (env novo), nenhum em usuários ainda.
