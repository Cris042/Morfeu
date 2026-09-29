# Refinamento — Épico E4 (Reserva — backend) — 2026-09-29

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 3 perguntas escaladas e **respondidas pelo usuário no mesmo dia**.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir | 2 tasks; **um único caminho** de criação/roubo: `INSERT ... ON CONFLICT (sessao_id, assento_codigo) WHERE status='ativo' DO UPDATE ... WHERE holds.expires_at < agora`; aggregate + VOs (`AssentoCodigo`, `StatusHold`) + repository manual na TX (ADR 0005); porta síncrona `reserva → sessao` em 1 chamada, sem cache; N assentos em 1 TX ordenados por código; extensão com guarda `extensoes_usadas = 0`; sweeper no worker sem evento; recomenda **ADR "Trava de assento"** |
| security | seguir com bloqueantes | teto de holds ativos por dono (inclusive no roubo e no lote); rate limit por IP e por dono reaproveitando o limitador do E1; token de carrinho opaco ≥ 128 bits (CSPRNG) com **SHA-256 em repouso**; transporte decidido explicitamente; posse checada em toda operação → **404** (nunca 403); ocupação expõe só `codigo + livre/ocupado`, inclusive no valor em cache |
| qa | seguir com ressalvas | corrida canônica N=20 + barreira, ≥ 10 rodadas, query de invariante; **teste dedicado do roubo de expirado** e de lotes em ordem cruzada (a classe de deadlock do E3); lote tudo-ou-nada sem hold órfão; aggregate com relógio injetado; sweeper com gatilho explícito + corrida sweeper × roubo; limites sem tempo real; defasagem do cache medida |
| sre-devops | seguir | ocupação em **endpoint separado**, composta no front; cache TTL 2–3 s **sem invalidação ativa**; índice parcial em `expires_at WHERE status='ativo'`; sweeper 30–60 s com advisory lock; métricas sem label de sessão/assento; alertas de 409 anormal e sweeper parado; purge de holds terminais = pendência |
| backend-dev | seguir | T1 ~18–22 arquivos, T2 ~8–10; `RETURNING` vazio (sem linha) = "não travou" → 409; teto por dono consistente com `pg_advisory_xact_lock` por dono na TX (sem tabela extra); repository como struct concreta sem interface; reaproveitar o **transporte** do refresh (cookie + `X-Requested-With`), não o token nem a tabela |

## Debate (divergências e resolução)

1. **Expirado ainda `ativo` bloqueando o índice parcial** — pré-check/UPDATE antes do INSERT × sweeper síncrono × upsert com guarda. → **Consenso**: upsert único com `DO UPDATE ... WHERE holds.expires_at <= agora` (roubo atômico; o `RETURNING` sem linha significa hold vivo de outro dono → 409). O hold roubado recebe **id novo** (referências antigas morrem) e o dono novo. `agora` vem do relógio injetado da aplicação (teste sem sleep — ADR 0006), não do `now()` do banco.
2. **Dono do hold: usuário autenticado × anônimo** — arquiteto propôs VO com duas variantes (`sub` do JWT | token anônimo). → **Ajuste no debate**: uma variante só no E4 — **o dono é sempre o token de carrinho** emitido pela `reserva`, logado ou não. O vínculo com conta/e-mail acontece no pedido (E6/E8, fluxo convidado+conta de doc.md §14.4); duas identidades para o mesmo hold dobrariam a superfície (IDOR) sem requisito que as exija (§1 anti-overengineering).
3. **Teto por dono sob concorrência** — `COUNT` simples deixa passar 2 requisições paralelas do mesmo dono. → **Consenso**: `pg_advisory_xact_lock` com chave derivada do hash do dono como 1ª instrução da TX de criação (serializa só o mesmo dono; solta no commit/rollback).
4. **Cache da ocupação: invalidação ativa × TTL curto** — security preferia invalidar por hold; sre/arquiteto/backend: sob disputa, invalidar a cada hold zera o cache justamente no pico. → **Consenso** (security aceita com a condição do modelo reduzido): **TTL 3 s sem invalidação**; a defasagem máxima (3 s) é menor que o intervalo de polling do E5 (3–5 s) e o 409 da trava é a verdade final. O valor em cache guarda só `codigo → livre|ocupado`.
5. **Ocupação: endpoint separado × composição no backend** — arquiteto sugeriu compor no `main`. → **Consenso**: `GET /sessoes/{id}/ocupacao` separado da `reserva`; o E5 busca o layout (`/sessoes/{id}/mapa`) uma vez e faz polling só da ocupação (payload menor, módulos independentes).
6. **ADR "Trava de assento"** → **Escalado**: usuário **autorizou o ADR 0008**.
7. **Transporte do token de carrinho** → **Escalado**: usuário escolheu **cookie HttpOnly/Secure/SameSite=Strict + header anti-CSRF** (mesmo mecanismo do refresh).
8. **Valores dos limites** → **Escalado**: usuário escolheu **6 holds ativos por dono; 30 req/min por IP e 20 req/min por dono** (criar e estender).

## Conclusão

2 tasks; **ADR 0008 — Trava de assento** criado com autorização. Nenhuma dependência nova.

### Exigências por task

#### T1 — Trava de assento (escrita) + sweeper

- Migration `holds`: `id uuid PK`, `sessao_id → sessoes`, `assento_codigo` (formato `^[A-Z][1-9][0-9]?$`), `dono_hash bytea` (SHA-256 do token), `status ∈ {ativo, liberado, expirado, convertido}`, `expires_at`, `extensoes_usadas 0..1`, timestamps. **`UNIQUE (sessao_id, assento_codigo) WHERE status='ativo'`** + índices parciais `(expires_at) WHERE status='ativo'` e `(dono_hash) WHERE status='ativo'`. A FK para `sessoes` é integridade declarada — a `reserva` **não lê** `sessoes`/`salas`.
- Porta `reserva.FonteSessoes`: 1 chamada devolve os códigos de assento de uma sessão **agendada e futura** (ou "indisponível"); implementada pelo `sessao`; sem cache.
- Aggregate/VOs (ADR 0005): `AssentoCodigo`, `StatusHold`, `Hold` (campos não exportados; `Estender`, `Liberar`, `Ativo` com relógio injetado); validação do lote em memória (1–6 códigos, sem repetição, todos no layout) antes do banco; repository manual (struct concreta) na TX.
- Travar lote: 1 TX; `pg_advisory_xact_lock(dono)`; conta holds vivos do dono (`status='ativo' AND expires_at > agora`) + lote ≤ 6 senão 409 `limite_holds`; upserts **em ordem crescente de código**; qualquer "sem linha" → rollback e 409 `assento_indisponivel` com os códigos recusados (tudo ou nada). TTL 10 min.
- Estender: +10 min uma única vez (`WHERE status='ativo' AND expires_at > agora AND extensoes_usadas = 0`) → 409 `extensao_esgotada`. Liberar → `liberado`. Listar meus holds ativos. Posse checada em toda operação: hold de outro dono, expirado ou inexistente → **404**.
- Token de carrinho: 32 bytes CSPRNG (base64url), emitido na 1ª trava sem cookie válido; cookie `morfeu_carrinho` HttpOnly, Secure, SameSite=Strict, Path `/`, sem Max-Age; **só o SHA-256 vai ao banco**; nunca em log. Rotas que mudam estado exigem `X-Requested-With: morfeu` (403 `csrf`).
- Rate limit (limitador do E1, Redis + fallback em memória): criar e estender → 30/min por IP e 20/min por dono → 429.
- Sweeper no worker (a cada 60 s): marca `expirado` em lotes, com `pg_try_advisory_xact_lock` (uma instância por vez); sem evento. Métricas por callback (domínio sem OTel): holds criados, indisponíveis (409 de corrida) e expirados pelo sweeper — sem label de sessão/assento.
- Depguard `reserva-domain` (strict); driver só em `internal/reserva/db/erros.go`.
- Testes: unit do aggregate/VOs (relógio fixo); integração com PG real: **corrida canônica N=20 no mesmo assento → exatamente 1 × 201 e 19 × 409, 10 rodadas + query de invariante**; roubo de expirado (relógio adiantado) inclusive sob corrida → exatamente 1; lote tudo-ou-nada sem hold órfão; lotes em ordem cruzada concorrentes → nenhum 500; teto de 6 com 2 requisições paralelas do mesmo dono; extensão única; posse → 404; CSRF 403; 429; sweeper com gatilho explícito + corrida sweeper × roubo; sessão cancelada/passada/inexistente → 404; código fora do layout → 400.

#### T2 — Ocupação (leitura) + cache + alertas

- `GET /sessoes/{id}/ocupacao` (público): lista de códigos **ocupados** (holds vivos por `expires_at > agora`); nada sobre dono/hold/expiração; sessão indisponível → 404 (via a mesma porta).
- Cache Redis `reserva:ocupacao:{sessao}` **TTL 3 s, sem invalidação ativa**, valor só com os códigos; `Cache-Control` curto coerente.
- Alertas (Prometheus): taxa anormal de 409 de trava; sweeper parado (contador de execuções sem aumento).
- Testes: contrato (sem dados de dono), hold expirado não aparece, defasagem ≤ TTL medida com relógio/Redis real, cache sobrevive a novo hold até expirar.

### Exigências transferidas

- **E5**: mapa compõe `/sessoes/{id}/mapa` (1×) + polling de `/sessoes/{id}/ocupacao` (3–5 s); SPA envia `X-Requested-With` e `credentials: 'include'`; contagem regressiva a partir de `expira_em`.
- **E6**: pedido converte holds do carrinho (`convertido`) na mesma TX; índice parcial equivalente em `ingressos`; teto de 6 por pedido; métrica de convertidos.
- **E12/hardening**: purge periódico de holds terminais (liberado/expirado) antigos.

### Perguntas escaladas ao usuário (respondidas em 2026-09-29)

1. ADR "Trava de assento" → **autorizado (ADR 0008)**.
2. Transporte do token de carrinho → **cookie HttpOnly + header anti-CSRF**.
3. Limites → **6 holds ativos por dono; 30 req/min por IP (20/min por dono)**.
