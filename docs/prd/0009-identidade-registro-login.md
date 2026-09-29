# PRD 0009 — Identidade: usuário, registro, login, seed do operador (E1, T1 parte 2/2)

- **Task:** docs/tasks/0009-identidade-registro-login.md
- **Branch:** feature/0009-identidade-registro-login
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Criar o módulo `internal/identidade` (transaction script — ADR 0005) sobre a plataforma da 0008: cadastro de cliente, login com Argon2id, anti-enumeração, limites de tentativa, `GET /auth/eu` e operador só por CLI. Fonte: `docs/refinamentos/E1-identidade.md` §T1 + respostas do usuário; `doc.md` §5, §7, §14.4.

## Escopo

Migration 005, módulo `identidade` (queries, service, senha, handler, errors), wiring em `main.go` (config, emissor, limitadores, métricas, rotas, CLI), config de env, depguard, smoke do CI e exemplos de env.

## Fora de escopo

Refresh token, logout, deleção de conta (0010). Verificação de e-mail, recuperação de senha, 2FA, CAPTCHA (Fase 2 / fora do MVP). Rotas do backoffice (E2).

## Requisitos funcionais

- RF01 — Migration 005 `usuario`: `id UUID PK`, `nome VARCHAR(120) NOT NULL`, `email TEXT NOT NULL` com `UNIQUE` e `CHECK (email = lower(email))`, `senha_hash TEXT NOT NULL`, `papel TEXT NOT NULL CHECK (papel IN ('cliente','operador'))`, `criado_em`/`atualizado_em TIMESTAMPTZ`. Ownership: `identidade`.
- RF02 — `POST /auth/registro` `{nome, email, senha}` → **201** `{id, nome, email, papel:"cliente"}`. Papel **sempre** `cliente` (campo `papel` no payload é ignorado — o DTO não o tem). Validação: nome 1–120 caracteres após trim; e-mail normalizado (trim + lowercase) e válido (`net/mail`, sem display name); senha 8–128 caracteres (runas). Erros: 400 `{"erro":"dados_invalidos","campos":[...]}`; e-mail em uso → **409** `{"erro":"email_em_uso"}`. Limite por IP: 10 cadastros / 1 h (429). Hash sob o semáforo (429 se saturado).
- RF03 — `POST /auth/login` `{email, senha}` → **200** `{access_token, token_type:"Bearer", expira_em}` (segundos). Fluxo: limitadores (conta 5/5 min, IP 20/5 min — bloqueado → **429** `{"erro":"muitas_tentativas"}`) → semáforo (`TryAcquire`; cheio → **429** `{"erro":"tente_novamente"}`) → busca por e-mail → Argon2id contra o hash do usuário **ou contra um hash dummy** gerado no boot com os mesmos parâmetros → falha: **401** `{"erro":"credenciais_invalidas"}` idêntico para e-mail inexistente e senha errada + `RegistrarFalha` em conta e IP; sucesso: `Limpar` da conta + token.
- RF04 — `GET /auth/eu` (Exigir cliente|operador) → 200 `{id, nome, email, papel}` do próprio usuário (lido do banco pelo `usuario_id` do token).
- RF05 — `morfeu seed-operador -nome <n> -email <e>`: cria operador com senha aleatória (24 caracteres, `crypto/rand`, alfabeto alfanumérico) impressa **uma vez** no stdout; e-mail já existente → mensagem "já existe", exit 0, nada muda (idempotente). Nenhuma rota HTTP cria/promove operador.
- RF06 — Argon2id: `argon2.IDKey`, salt 16 B `crypto/rand`, chave 32 B, formato PHC `$argon2id$v=19$m=<KiB>,t=<n>,p=<n>$<salt>$<hash>` (base64 sem padding). Verificação lê os parâmetros do próprio hash (rehash futuro sem migração) e compara em tempo constante. Parâmetros por env: `ARGON2_MEMORIA_KIB` (19456), `ARGON2_ITERACOES` (2), `ARGON2_PARALELISMO` (1).
- RF07 — Semáforo de hashing: canal bufferizado com capacidade `HASH_CONCORRENCIA` (padrão `runtime.NumCPU()`); usado em registro e login.
- RF08 — Métricas (0008): `Login(resultado, duração)` e `Bloqueio(escopo)` em cada tentativa.
- RF09 — Config: `JWT_SEGREDO` (≥ 32 bytes; obrigatório em `-mode=api|all` — boot falha com mensagem que não imprime o valor), `JWT_KID` (padrão `k1`). Echo com `IPExtractor = ExtractIPDirect` (sem proxy confiável até a E0c-CD, onde passa a ler `X-Forwarded-For` do Caddy). `BodyLimit` de 16 KB no grupo `/auth`.

## Requisitos não funcionais

- RNF01 — Fronteiras (ADR 0003): `identidade` não importa driver (`pgx`/`pgconn`) nem `redis`/`jwt`; usa `internal/identidade/db`, `internal/autenticacao`, `x/crypto/argon2`. Nenhum pacote fora de `internal/identidade` e `cmd/morfeu` importa `identidade` (depguard). Consultas evitam `pgx.ErrNoRows` (`:many`/`:execrows`) para o domínio não depender do driver.
- RNF02 — Privacidade: logs de auth só com `usuario_id`/resultado — nunca e-mail, senha ou token; JSON malformado não ecoa o corpo.
- RNF03 — Testes (ADR 0006): unit para senha/validação; integração com PG + Redis reais pelas rotas HTTP.

## Regras de negócio

- RN01 — Cliente se registra sozinho; operador só por CLI (doc.md §2).
- RN02 — E-mail é único e comparado normalizado.
- RN03 — Mensagens de falha de login não distinguem "conta inexistente" de "senha errada".

## Critérios de aceite

- [ ] CA01 — Registro feliz (201, papel cliente mesmo com `"papel":"operador"` no JSON); duplicado (409, inclusive variando caixa/espaços); inválidos (400 por campo: nome vazio, e-mail inválido, senha 7 e 129 caracteres).
- [ ] CA02 — Login feliz: token validado pelo `Emissor` com `sub` = id e `papel` = cliente.
- [ ] CA03 — Contrato anti-enumeração: status, corpo e headers relevantes **idênticos** para e-mail inexistente × senha errada; teste de unidade prova que o verificador de hash roda nos dois ramos.
- [ ] CA04 — Limites: 5 falhas por conta → 6ª tentativa 429 (mesmo com senha certa); IP com 20 falhas → 429 para outra conta; sucesso zera a conta.
- [ ] CA05 — Semáforo saturado (capacidade 1 ocupada) → 429 `tente_novamente`.
- [ ] CA06 — `GET /auth/eu`: sem token 401; cliente 200 com os próprios dados; operador 200.
- [ ] CA07 — `SeedOperador` cria 1× e é idempotente; senha retornada tem 24 caracteres e autentica; nenhuma rota registrada contém criação de operador (inspeção das rotas do Echo).
- [ ] CA08 — Senha nunca aparece nos logs (observer) em registro, login com falha e JSON malformado.
- [ ] CA09 — Argon2id: formato PHC, parâmetros lidos do hash, hash malformado → erro, salts distintos para a mesma senha.
- [ ] CA10 — Config: `JWT_SEGREDO` curto/ausente falha ao montar o emissor; migration 005 up→down→up (CI); CI verde.

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Argon2id PHC, verificação, malformado, salts | unit | CA09 |
| Validação de entrada e normalização | unit | CA01 |
| Ramos do login chamam o verificador (contador no teste) | unit | CA03 |
| Rotas `/auth/*` com PG + Redis reais | integração | CA01–CA08 |
| Config JWT | unit (config) | CA10 |

## Plano de implementação

1. lib.md (`x/crypto` direta, 0.55.0 — última compatível com Go 1.25).
2. Migration 005 + queries + sqlc.
3. `senha.go`, `errors.go`, `service.go`, `handler.go`.
4. Config + main (emissor, limitadores, métricas, semáforo, rotas, CLI, IPExtractor, BodyLimit).
5. Depguard, ci.yml, exemplos de env.
6. Testes; lint; -race; gate.

**Skills de apoio (§4.4):** `security-review`, `golang-database`.

## Arquivos que serão criados

- `migrations/005_usuario.up.sql`, `migrations/005_usuario.down.sql`
- `internal/identidade/queries.sql`, `internal/identidade/db/{db.go,models.go,queries.sql.go}`
- `internal/identidade/senha.go`, `service.go`, `handler.go`, `errors.go`
- `internal/identidade/senha_test.go`, `internal/identidade/identidade_integration_test.go`
- `docs/prd/0009-identidade-registro-login.md`

## Arquivos que serão modificados

- `cmd/morfeu/main.go`, `internal/config/config.go`, `internal/config/config_test.go`
- `.golangci.yml`, `sqlc.yaml`, `.github/workflows/ci.yml`, `.env.example`, `.env.docker-compose.example`
- `go.mod`, `go.sum`, `lib.md`
- `docs/tasks/0009-…`, `docs/tasks/README.md`, `plan.md`, `state.md`

Total previsto: 29.

## Dependências utilizadas

`golang.org/x/crypto` (argon2) promovida a direta, **v0.55.0** — a 0.56.0 exige Go 1.26 (subiria o toolchain do projeto, ADR 0001); advisories restantes são em `ssh`/`openpgp`, não usados. Existentes: echo, uuid, zap, sqlc/pgx (só no `db` gerado), `internal/autenticacao`.

## Impactos técnicos

- Primeira escrita pública da API; `JWT_SEGREDO` passa a ser obrigatório para subir a API (compose/CI/env de exemplo atualizados).
- Tabela nova; nenhuma alteração em tabelas existentes.

## Riscos

- Timing residual (ex.: validação de tamanho antes do hash) → só depende do input do atacante, não da existência da conta.
- IP spoofing via `X-Forwarded-For` → extrator direto até existir proxy confiável (E0c-CD troca e documenta).
- Argon2id na A1 sem calibração → parâmetros por env + recalibração no checklist da E0c-CD.

## Estratégia de rollback

Reverter o merge; `migrate down 1` executa `005_usuario.down.sql` (`DROP TABLE usuario`). Sem dados de produção ainda.
