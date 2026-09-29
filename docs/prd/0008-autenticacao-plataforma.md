# PRD 0008 — Autenticação (plataforma): JWT, middleware de papel, limitador de tentativas (E1, T1 parte 1/2)

- **Task:** docs/tasks/0008-autenticacao-plataforma.md
- **Branch:** feature/0008-autenticacao-plataforma
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Entregar `internal/autenticacao`, a peça de plataforma que emite/valida o access token e autoriza por papel, usada por qualquer módulo sem importar `identidade` (ADR 0003), mais o limitador de tentativas de login com fallback em memória decidido no refinamento. Fonte: `docs/refinamentos/E1-identidade.md` §T1 e respostas do usuário (2026-09-29); `doc.md` §14.4.

## Escopo

Pacote `internal/autenticacao` completo e testado; instrumentos de métrica de auth; allowlist de labels; depguard; registro da dependência. Nenhuma rota nem wiring (task 0009 consome).

## Fora de escopo

Tabela `usuario`, registro/login, seed, `main.go`, config de env (0009); refresh/logout/pseudonimização (0010); rotação automática de chave (Fase 2 — `kid` já nasce aqui).

## Requisitos funcionais

- RF01 — `NovoEmissor(ConfigJWT{Segredo []byte, Kid string, TTL time.Duration, Agora func() time.Time}) (*Emissor, error)`: rejeita segredo < 32 bytes, `Kid` vazio ou TTL ≤ 0 (`ErrConfigInvalida`). TTL de produção: **10 min** (constante `TTLAccessPadrao`).
- RF02 — `Emissor.Emitir(usuarioID uuid.UUID, papel Papel) (token string, expiraEm time.Time, err error)`: HS256, header `kid`, claims **só** `sub`, `papel`, `iat`, `exp` (sem PII). `Papel` ∈ {`cliente`, `operador`} (tipo próprio; valor desconhecido → erro).
- RF03 — `Emissor.Validar(token string) (*Claims, error)`: `jwt.WithValidMethods(["HS256"])`, `WithExpirationRequired`, `WithIssuedAt`, `WithTimeFunc(Agora)`, `kid` precisa ser igual ao configurado, `sub` precisa ser UUID, `papel` precisa ser válido; qualquer falha → `ErrTokenInvalido` (causa só em `errors.Is`, nunca exposta ao cliente).
- RF04 — `Exigir(e *Emissor, papeis ...Papel) echo.MiddlewareFunc` (fail-closed): header `Authorization: Bearer <token>` ausente/malformado/inválido → **401** `{"erro":"nao_autenticado"}` com `WWW-Authenticate: Bearer`; papel fora da lista → **403** `{"erro":"acesso_negado"}`; **lista vazia → 403 sempre** (rota nunca fica aberta por esquecimento). Sucesso: grava `usuario_id` e `papel` no contexto Echo.
- RF05 — Helpers `UsuarioID(c) (uuid.UUID, bool)` e `PapelDe(c) (Papel, bool)` para handlers de qualquer módulo.
- RF06 — `NovoLimitador(ConfigLimitador{Redis redis.Cmdable, Prefixo string, Max int, Janela time.Duration, Agora func() time.Time}, logger)`: `Bloqueado(ctx, chave) bool` (≥ Max falhas na janela) e `RegistrarFalha(ctx, chave)`; `Limpar(ctx, chave)` no sucesso. Redis: `INCR` + `EXPIRE` na 1ª falha (pipeline). **Erro do Redis → contador em memória** (mapa com expiração pelo relógio injetável, mutex, limpeza preguiçosa) + log `warn` com a causa (sem a chave, que pode conter e-mail); nunca libera sem limite, nunca devolve erro ao chamador. Chaves recebidas já prefixadas pelo chamador por escopo (`conta:`/`ip:`), e o limitador aplica `Prefixo` + **SHA-256 da chave** antes de gravar (nenhum e-mail/IP em claro no Redis).
- RF07 — Métricas (OTel API; registradas no MeterProvider global da 0006): `auth_login_total{resultado}` (counter), `auth_login_duracao_segundos` (histograma), `auth_ratelimit_bloqueios_total{escopo}` (counter), `auth_refresh_reuso_total` (counter, usado na 0010). Tipo `Metricas` com métodos `Login(resultado, dur)`, `Bloqueio(escopo)`, `ReusoRefresh()`. Valores de `resultado` fechados: `sucesso`, `credenciais_invalidas`, `bloqueado`, `saturado`; `escopo`: `conta`, `ip`.

## Requisitos não funcionais

- RNF01 — Segurança: sem PII em claims, logs ou chaves do Redis; segredo nunca logado; comparação de `kid` exata; mensagens de erro HTTP genéricas.
- RNF02 — Fronteiras: `internal/autenticacao` é plataforma (pode usar echo, redis, jwt, otel, zap); nenhum domínio importa `internal/identidade` (regra depguard entra na 0009 junto do pacote); `catalogo` segue sem acesso a `jwt`/redis.
- RNF03 — Telemetria: `resultado` e `escopo` entram na allowlist de labels (0006) — teste de cardinalidade existente continua valendo.
- RNF04 — Testabilidade: relógio injetável em `Emissor` e `Limitador`; zero `time.Sleep` nos testes.

## Regras de negócio

- RN01 — Papéis válidos: `cliente`, `operador` (doc.md §5). Autorização é decidida só pela claim `papel`.
- RN02 — Limites de tentativas (valores usados pela 0009; o limitador é genérico): **5 falhas / 5 min por conta**, **20 falhas / 5 min por IP**.

## Critérios de aceite

- [ ] CA01 — Token emitido decodifica com header `{alg: HS256, kid, typ}` e payload com exatamente as chaves `sub`, `papel`, `iat`, `exp`; `exp - iat` = 10 min.
- [ ] CA02 — `Validar` rejeita: `alg: none`, HS384/HS512, assinatura com outro segredo, token adulterado, expirado (relógio avançado), sem `exp`, `kid` diferente/ausente, `sub` não-UUID, `papel` inválido.
- [ ] CA03 — Matriz do middleware (tabela no teste): {sem header, `Basic`, Bearer malformado, expirado, adulterado, cliente válido, operador válido} × {`Exigir(cliente)`, `Exigir(operador)`, `Exigir(cliente, operador)`, `Exigir()`} → status exato; contexto populado só no 200.
- [ ] CA04 — Limitador com Redis real: 5 falhas → bloqueado; relógio + janela → liberado (TTL real verificado por `PTTL` > 0); chave no Redis é hash (não contém o texto original).
- [ ] CA05 — Limitador com Redis indisponível: comportamento idêntico usando memória; sem erro devolvido; log `warn`.
- [ ] CA06 — `NovoEmissor` recusa segredo de 31 bytes, kid vazio, TTL 0.
- [ ] CA07 — Métricas aparecem no `/metrics` com labels da allowlist (teste com a telemetria real da 0006).
- [ ] CA08 — CI verde (lint, -race, govulncheck).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Emitir/decodificar partes do JWT | unit | CA01, RF02 |
| Tabela de tokens hostis | unit | CA02, RF03 |
| Matriz Exigir × tokens (Echo + httptest) | unit | CA03, RF04, RF05 |
| Config inválida | unit | CA06 |
| Limitador em memória com relógio fake | unit | RF06 |
| Limitador com Redis (testcontainers) e com Redis inalcançável | integração | CA04, CA05 |
| Métricas no registry da telemetria | unit | CA07, RNF03 |

## Plano de implementação

1. `lib.md` + `go get github.com/golang-jwt/jwt/v5@v5.3.1`.
2. `jwt.go` (Papel, Claims, Emissor), `middleware.go` (Exigir + helpers).
3. `limitador.go`, `metricas.go`.
4. Allowlist na telemetria; depguard.
5. Testes; lint; -race; gate.

**Skills de apoio (§4.4):** `security-review`, `golang-testing`.

## Arquivos que serão criados

- `internal/autenticacao/jwt.go`, `middleware.go`, `limitador.go`, `metricas.go`
- `internal/autenticacao/autenticacao_test.go` (unit: CA01–CA03, CA06, CA07, limitador em memória)
- `internal/autenticacao/limitador_integration_test.go` (CA04, CA05)
- `docs/prd/0008-autenticacao-plataforma.md`

## Arquivos que serão modificados

- `internal/telemetria/telemetria.go` — `resultado`, `escopo` na allowlist.
- `.golangci.yml` — nenhum domínio importa `jwt` diretamente (só via `autenticacao`).
- `go.mod`, `go.sum`, `lib.md`.
- `docs/tasks/0008-…`, `docs/tasks/README.md`, `plan.md`, `state.md`; já no branch: `docs/refinamentos/E1-identidade.md`, `docs/refinamentos/README.md`.

Total previsto: ~17.

## Dependências utilizadas

Nova: `github.com/golang-jwt/jwt/v5` **v5.3.1** (última; GO-2025-3553 corrigida em 5.2.2; OSV sem advisory aberto p/ 5.3.1 em 2026-09-29). Existentes: echo, go-redis, otel metric API, zap, uuid, testcontainers (Redis via `GenericContainer`, padrão já usado).

## Impactos técnicos

Novo pacote de plataforma; nenhuma rota; nenhuma mudança de comportamento do binário. `/metrics` só mostra as séries `auth_*` quando houver observações (0009).

## Riscos

- Abstração sem consumidor por uma task → contrato coberto por testes; consumida na 0009.
- Fallback em memória diverge entre instâncias → aceitável: 1 instância no MVP (decisão do usuário).

## Estratégia de rollback

Reverter o merge; nada consome o pacote ainda.
