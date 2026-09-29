# Refinamento — Épico E1 (Identidade) — 2026-09-29

Cerimônia por épico (roles.md §6.14): brief pré-digerido único → pareceres independentes (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev` com os 4) → debate → 4 perguntas escaladas e **respondidas pelo usuário no mesmo dia**. Tasks previstas: **T1** registro/login + JWT + RBAC + seed do operador; **T2** refresh rotativo com detecção de reuso + deleção por pseudonimização.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | `identidade` = transaction script (ADR 0005), dono de `usuario`/`refresh_token`; **middleware JWT e tipo de claims em plataforma** (zero import de `identidade` por outros módulos); seed = subcomando CLI (padrão `criar-filme`); RBAC = claim `papel` (sem policy engine); access JWT + refresh opaco no PG é o mínimo p/ rotação/reuso; rate limit em Redis (contador descartável = uso de cache); pseudonimização preserva `id`/FK e libera o e-mail |
| security | seguir com ressalvas | Argon2id explícito (m≥19MiB, t=2, p=1, salt 16B, PHC); JWT com allowlist de `alg`, claims sem PII, segredo ≥32B via env, `kid` p/ invalidação em massa; RBAC fail-closed; anti-enumeração com hash dummy; rate limit IP+conta; limite de tamanho de senha; refresh 256 bits + SHA-256; cookie httpOnly/Secure/SameSite/Path; sem janela de graça; logout revoga família; header anti-CSRF; pseudonimização revoga famílias e não vaza PII em eventos |
| qa | seguir com ressalvas | matriz RBAC exaustiva no PRD; anti-enumeração por **contrato de resposta idêntica** + teste de que ambos os ramos executam o hash (timing estatístico só como smoke não-bloqueante); seed sem rota HTTP (teste negativo); corrida de 2 refresh com barreira + PG real, 20× sob `-race`; oráculo exato da pseudonimização; **clock injetável** (sem sleep); fixtures por teste |
| sre-devops | seguir com ressalvas | Argon2id compete por CPU com o worker (`-mode=all`) → **semáforo de hashing** + 429; chave JWT via env, rotação manual (débito); métricas `auth_*` sem PII/labels altos; alerta de reuso de refresh; limpeza de refresh expirados no worker existente; pseudonimização não pode deixar PII em cache |
| backend-dev | seguir com ressalvas | viável; **T1 ≈ 18–25 arquivos, T2 ≈ 6–8** (sem divisão); pacote `internal/autenticacao`; nova regra depguard p/ `identidade`; semáforo barato; fail-closed no rate limit; 4 métricas agora, nada por usuário/IP |

## Debate (divergências e resolução)

1. **Rate limit com Redis indisponível** — arquiteto: fail-open (Redis é só cache) × sre/backend-dev: fail-closed (login é superfície de brute-force). → **Escalado ao usuário** (decisão arquitetural relevante, §6.1): **fallback em memória** (throttle local por processo quando o Redis falha) — nunca fica sem limite, nunca derruba o login.
2. **Semáforo de hashing** — custo × proteção. → Consenso (§6.8 resiliência, §1 anti-overengineering): entra, com **canal bufferizado da stdlib** (sem `x/sync`, zero dependência nova); excedente → 429 antes do Argon2id.
3. **Parâmetros Argon2id sem VM para calibrar** — sre sugeriu esperar a VM. → Consenso: mínimo OWASP agora (m=19MiB, t=2, p=1) **configurável por env**; recalibração é item do checklist da E0c-CD.
4. **Algoritmo e rotação do JWT** — HS256 × EdDSA; rotação automática × manual. → Consenso: **HS256** (emissor e verificador são o mesmo binário), allowlist fixa, `kid` no header desde já; rotação manual documentada como débito (Fase 2).
5. **Nome/lugar do middleware** — `autenticacao` × `plataforma/auth`. → Consenso: **`internal/autenticacao`** (plataforma; PT técnico descritivo, ADR 0004); claims nesse pacote; domínios não importam `identidade`.
6. **Métricas** — lista do sre × risco de overengineering. → Consenso: 4 métricas (`auth_login_total{resultado}`, `auth_login_duracao_segundos`, `auth_refresh_reuso_total`, `auth_ratelimit_bloqueios_total{escopo}`) + 1 alerta (reuso > 0). Labels entram na allowlist da telemetria (0006).
7. **TTLs** — 10–15 min / 7–30 d. → **Escalado**: usuário escolheu **access 10 min / refresh 7 dias**.
8. **SameSite** — Strict × Lax. → **Escalado**: **Strict** (SPA e API same-origin).
9. **Corrida de refresh (2 abas)** — janela de graça × revogação. → **Escalado**: **sem janela de graça**; o 1º serializado vence, o 2º é reuso e revoga a família; **o SPA (E8) faz single-flight** do refresh entre abas (exigência transferida ao E8).

## Conclusão

Escopo do épico confirmado em 2 tasks, sem divisão (T1 ≤ 25 arquivos). Nenhum ADR novo: as decisões cabem nos ADRs 0003/0005 (fronteiras e transaction script) e no `doc.md` §14.4 — o refinamento só as detalha.

### Exigências por task

#### T1 — Registro/login + JWT + RBAC + seed do operador

- **Anatomia**: `internal/identidade` (handler → service → sqlc, transaction script), dona da tabela `usuario`; **`internal/autenticacao`** (plataforma): emissão/validação de JWT, `Claims`, middleware Echo que popula `usuario_id`/`papel` no contexto, helper de exigência de papel. Nenhum módulo importa `identidade` (regra depguard nova + regra `identidade-domain` strict).
- **Rotas**: `POST /auth/registro` (sempre papel `cliente` — campo `papel` do payload ignorado/rejeitado, teste explícito), `POST /auth/login`. **Operador só por CLI** `morfeu seed-operador` (idempotente por e-mail; senha aleatória ≥ 20 chars exibida **uma vez** no stdout, nunca logada); teste negativo de que não há rota HTTP equivalente.
- **Argon2id**: m=19MiB, t=2, p=1, salt 16B `crypto/rand`, formato PHC; parâmetros configuráveis por env; senha 8–128 caracteres validada **antes** do hash; e-mail normalizado (trim + lowercase).
- **Anti-enumeração**: e-mail inexistente executa Argon2id contra hash dummy; resposta de falha **idêntica** (status, corpo, headers) — oráculo do teste é o contrato, não o tempo; teste de unidade garante o hash nos dois ramos.
- **JWT**: HS256, allowlist de `alg` (teste negativo com `alg` diferente/`none`), `kid` no header, claims só `sub`/`papel`/`iat`/`exp`, **TTL 10 min**, segredo ≥ 32 bytes via env (boot falha sem ele; nunca em log/commit).
- **RBAC fail-closed**: rota protegida sem papel declarado nega; **matriz papel × rota no PRD** testada com {sem token, cliente, operador, expirado, adulterado} → 401/403 exatos.
- **Rate limit**: por conta e por IP (valores no PRD; referência 5 tentativas / 5 min por conta), Redis INCR+TTL, **fallback em memória** quando o Redis falha (teste com Redis indisponível); relógio injetável.
- **Semáforo de hashing**: capacidade = `NumCPU` (configurável), `TryAcquire` → 429 genérico.
- **Observabilidade**: `auth_login_total{resultado}`, `auth_login_duracao_segundos`, `auth_ratelimit_bloqueios_total{escopo}`; logs só com `usuario_id` (nunca e-mail/senha) — teste de que a senha não aparece em log nem em erro de binding.
- **Deps**: `golang-jwt/jwt/v5` ≥ 5.2.2 e `golang.org/x/crypto` (argon2) promovida a direta — registrar no `lib.md` antes do import; govulncheck é a fonte de verdade.

#### T2 — Refresh rotativo + detecção de reuso + pseudonimização

- Tabela `refresh_token` (dona: `identidade`): `id`, `usuario_id`, `familia_id`, `hash` (SHA-256 do token de 32 bytes `crypto/rand`, base64url), `expira_em` (**7 dias**), `usado_em`, `revogado_em`; token em claro **nunca** persistido.
- Rotas: `POST /auth/refresh` (cookie `httpOnly; Secure; SameSite=Strict; Path=/auth/refresh` + header anti-CSRF obrigatório, ex. `X-Requested-With`), `POST /auth/logout` (revoga a família server-side), `DELETE /auth/conta` (pseudonimização).
- **Rotação**: cada refresh marca o atual como usado e emite sucessor na mesma família, numa transação. **Reuso** (token usado/revogado reapresentado) → revoga a família inteira, 401, log `warn` `refresh_reuso_detectado` com `familia_id` (nunca o token) e `auth_refresh_reuso_total` + alerta Grafana (reuso > 0).
- **Corrida**: sem janela de graça — 2 refresh simultâneos com o mesmo token ⇒ exatamente 1 sucesso, o outro é reuso e a família termina revogada (oráculo do teste; barreira + PG real, 20× sob `-race`). Exigência transferida ao **E8**: single-flight do refresh no SPA entre abas.
- **Pseudonimização**: preserva `id`/`papel`; valores exatos definidos no PRD (ex.: `nome = 'Conta removida'`, `email = 'removido+<id>@invalido.local'`), senha_hash invalidado; revoga todas as famílias; nenhum evento/outbox/cache carrega PII; e-mail original liberado para novo cadastro; verificável por query.
- **Limpeza**: goroutine periódica no worker existente (`-mode=worker|all`) removendo refresh expirados/revogados antigos — sem serviço novo.

### Riscos priorizados

1. Brute-force/DoS de CPU no login → rate limit com fallback + semáforo + Argon2id configurável.
2. Roubo de refresh → rotação + reuso revoga família + cookie Strict/httpOnly + alerta.
3. Vazamento de PII (logs, JWT, eventos) → claims sem PII, redação no logger (0006), testes dedicados.
4. Acoplamento entre módulos via auth → `internal/autenticacao` + depguard.

### Perguntas escaladas ao usuário (respondidas em 2026-09-29)

1. Rate limit com Redis fora → **fallback em memória**.
2. TTLs → **access 10 min / refresh 7 dias**.
3. SameSite → **Strict**.
4. Corrida de refresh → **sem janela de graça + single-flight no SPA (E8)**.
