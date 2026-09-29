# PRD 0010 — Refresh rotativo com detecção de reuso + deleção por pseudonimização (E1, T2)

- **Task:** docs/tasks/0010-refresh-pseudonimizacao.md
- **Branch:** feature/0010-refresh-pseudonimizacao
- **Data:** 2026-09-29
- **Status:** ativo

## Objetivo

Fechar o E1 com sessão longa segura e deleção de conta conforme LGPD. Fonte: `docs/refinamentos/E1-identidade.md` §T2 e as respostas do usuário (refresh de 7 dias, `SameSite=Strict`, **sem janela de graça**, single-flight no SPA do E8); `doc.md` §7 (pseudonimização) e §14.4.

## Escopo

Migration 006; emissão do refresh no login; rotação com detecção de reuso; logout; deleção por pseudonimização; limpeza periódica no worker; métrica e alerta de reuso; testes.

## Fora de escopo

Single-flight do refresh entre abas (SPA, E8); rotação de chave JWT e "sair de todos os dispositivos" (Fase 2); deleção física (a trilha transacional futura referencia `usuario.id`).

## Requisitos funcionais

- RF01 — Migration 006 `refresh_token`: `id UUID PK`, `usuario_id UUID NOT NULL REFERENCES usuario(id)`, `familia_id UUID NOT NULL`, `hash BYTEA NOT NULL UNIQUE` (SHA-256), `expira_em`, `usado_em`, `revogado_em`, `criado_em`. Índices: `usuario_id` (FK + revogação por usuário), `familia_id` (revogação por família), `expira_em` (limpeza). Ownership: `identidade`.
- RF02 — Token: 32 bytes de `crypto/rand` em base64url (256 bits); só o SHA-256 é persistido; **validade 7 dias** (decisão do usuário).
- RF03 — Login (0009) passa a abrir uma família nova e setar o cookie `morfeu_refresh` com `HttpOnly; Secure; SameSite=Strict; Path=/auth/refresh; Max-Age=604800`. O corpo da resposta não muda (o access continua só no corpo, guardado em memória pelo SPA).
- RF04 — `POST /auth/refresh`: exige o header `X-Requested-With: morfeu` (anti-CSRF; sem ele → **403** `{"erro":"csrf"}`) e o cookie. Numa única TX: `SELECT ... FOR UPDATE` pelo hash → não existe ou expirado → **401**; `usado_em`/`revogado_em` preenchido → **reuso**: revoga a família inteira, incrementa `auth_refresh_reuso_total`, log `warn` `refresh_reuso_detectado` com `familia_id` (nunca o token) → **401** e cookie limpo; senão marca `usado_em`, insere o sucessor na mesma família e devolve um novo access (papel lido do `usuario`) + um novo cookie.
- RF05 — Corrida: dois refreshes simultâneos com o mesmo token serializam no `FOR UPDATE`; o primeiro rotaciona e o segundo vê `usado_em`, é tratado como reuso e revoga a família — **exatamente 1 sucesso, família revogada** (sem janela de graça, decisão do usuário).
- RF06 — `POST /auth/refresh/logout` (mesmo header anti-CSRF): revoga a família do cookie (idempotente, sem cookie → 204) e limpa o cookie → **204**. **Desvio da rota do refinamento** (`/auth/logout`): o cookie tem `Path=/auth/refresh` (exigência de security), então o logout precisa estar sob esse path para recebê-lo.
- RF07 — `DELETE /auth/conta` (`Exigir(cliente)`; o operador não se autoexclui pela API → 403): numa TX, `nome = 'Conta removida'`, `email = 'removido+<id>@invalido.local'` (único e minúsculo, libera o e-mail original), `senha_hash = '!'` (formato inválido: nenhum login casa), `atualizado_em = now()`, e revoga **todas** as famílias do usuário → **204** e cookie limpo. Nenhum evento de outbox é emitido (sem consumidor; não haveria PII de qualquer forma).
- RF08 — Limpeza: goroutine no worker (`-mode=worker|all`), a cada 1 h, apaga refresh com `expira_em < now()`, registrada no WaitGroup do shutdown.
- RF09 — Alerta Grafana "Reuso de refresh token detectado": `increase(auth_refresh_reuso_total[15m]) > 0`, severidade `critical` (7ª regra — acréscimo autorizado pelo refinamento E1, fora da lista fechada original do E0d).

## Requisitos não funcionais

- RNF01 — Nenhum token em claro no banco, em log, em métrica ou em erro.
- RNF02 — Fronteiras: `identidade` usa `outbox.WithTx`/`Tx`/`Pool` (helper de transação da plataforma, ADR 0002) — depguard `identidade-domain` passa a permitir `internal/outbox`.
- RNF03 — Testes com PG real; a corrida é repetida 20× sob `-race` usando barreira (sem sleep).

## Regras de negócio

- RN01 — Reapresentar um refresh já usado ou revogado é tratado como roubo: a família inteira morre.
- RN02 — Conta removida não autentica mais e não carrega PII; o `id` é preservado.

## Critérios de aceite

- [ ] CA01 — Login seta o cookie com todos os atributos; no banco há 1 linha com `hash` = SHA-256 do valor do cookie e o valor em claro não aparece.
- [ ] CA02 — Refresh válido: 200, novo access válido, novo cookie diferente; o antigo fica com `usado_em`; o sucessor está na mesma família.
- [ ] CA03 — Sem `X-Requested-With` → 403 (e nada muda no banco).
- [ ] CA04 — Reuso do token antigo depois da rotação → 401; família toda revogada (o sucessor também para de funcionar); métrica incrementada.
- [ ] CA05 — Corrida: 20 iterações × 2 goroutines com barreira → sempre 1×200 + 1×401 e família revogada.
- [ ] CA06 — Logout → 204, família revogada, cookie limpo (`Max-Age=0`); refresh posterior → 401.
- [ ] CA07 — Deleção → 204; linha com os valores exatos pseudonimizados; login com as credenciais antigas → 401; refresh → 401; o mesmo e-mail pode se cadastrar de novo; operador → 403.
- [ ] CA08 — Limpeza remove só os expirados.
- [ ] CA09 — Teste da stack: 7 regras de alerta; CI verde (migration 006 up→down→up).

## Plano de testes

| Cenário | Tipo | Cobre |
|---|---|---|
| Rotas `/auth/*` com PG + Redis reais | integração | CA01–CA04, CA06–CA08 |
| Corrida com barreira, 20× | integração (`-race`) | CA05 |
| Provisioning do Grafana | integração (stack) | CA09 |

## Plano de implementação

1. Migration 006 + queries + sqlc.
2. `sessao.go` (gerar/abrir família, rotacionar, revogar, pseudonimizar, limpar).
3. Handler (cookie, refresh, logout, conta) + login emitindo o cookie.
4. main (limpeza no worker), depguard, alerta, teste da stack, runbook.
5. Testes; lint; -race; gate.

**Skills de apoio (§4.4):** `golang-database`, `security-review`.

## Arquivos que serão criados

- `migrations/006_refresh_token.up.sql`, `.down.sql`
- `internal/identidade/sessao.go`
- `internal/identidade/sessao_integration_test.go`
- `docs/prd/0010-refresh-pseudonimizacao.md`, `docs/tasks/0010-refresh-pseudonimizacao.md`

## Arquivos que serão modificados

- `internal/identidade/queries.sql`, `db/models.go`, `db/queries.sql.go` — refresh + pseudonimização.
- `internal/identidade/service.go` (login abre família; `Servico` recebe o pool), `handler.go` (rotas e cookie).
- `sqlc.yaml` (schema 006 no bloco identidade), `cmd/morfeu/main.go` (pool no serviço + limpeza no worker), `.golangci.yml` (`identidade-domain` + `internal/outbox`).
- `configs/grafana/provisioning/alerting/alertas.yml`, `test/observabilidade/stack_integration_test.go`, `docs/observabilidade.md`.
- `docs/tasks/README.md`, `plan.md`, `state.md`.

Total previsto: ~22.

## Dependências utilizadas

Nenhuma nova.

## Impactos técnicos

- A resposta do login ganha `Set-Cookie`; o contrato do corpo é mantido.
- O worker ganha uma goroutine de limpeza.
- Tabela nova com FK para `usuario`.

## Riscos

- A serialização da corrida depende do `FOR UPDATE` na mesma TX → prova em CA05.
- Cookie `Secure` em dev http: navegadores tratam `localhost` como contexto seguro; os testes leem o `Set-Cookie`.

## Estratégia de rollback

Reverter o merge; `migrate down 1` (`DROP TABLE refresh_token`). Refresh tokens em circulação deixam de valer (o usuário faz login de novo). Contas já pseudonimizadas continuam assim — a operação é irreversível por design (LGPD).
