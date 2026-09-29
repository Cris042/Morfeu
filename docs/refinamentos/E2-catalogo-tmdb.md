# Refinamento — Épico E2 (Catálogo + TMDB) — 2026-09-29

Cerimônia por épico (roles.md §6.14): brief único → pareceres (`arquiteto`, `security`, `qa`, `sre-devops`, depois `backend-dev`) → debate → 1 pergunta escalada e **respondida pelo usuário no mesmo dia** (pôster por hotlink). Marco: **M2 — operador importa filme real**.

## Pareceres

| Agente | Posição | Pontos-chave |
|---|---|---|
| arquiteto | seguir com ressalvas | 2 tasks; migrar `films`→`filmes` agora (antes da FK do E3), id por IDENTITY, projeção junto; arquivar (`arquivado_em`) em vez de deletar; cache por DELETE síncrono; port `ImportadorFilme` + adapter `internal/catalogo/tmdb` + fake httptest; upsert por `tmdb_id`; atribuição TMDB no retorno |
| security | seguir com ressalvas | backoffice `Exigir(operador)`; limites de tamanho e faixa de duração; soft delete; cache por evento (divergente); token v4 no header, host fixo, nada de segredo em log; pôster hotlink com host validado; dados do TMDB não confiáveis; stdlib; rate limit do import = overengineering; exigência futura ao SPA: nunca `dangerouslySetInnerHTML` |
| qa | seguir com ressalvas | migration up→down→up com os seeds; regressão E0a (GET /filmes, outbox→projeção); matriz de autorização; concorrência no INSERT; adapter só contra httptest (grep); retry com assert de tentativas; invalidação verificada no Redis; **rejeitar filme sem duração** |
| sre-devops | seguir com ressalvas | IDENTITY; timeout 3–5 s, retry ≤ 3 com backoff+jitter (5xx/rede/timeout/429 + Retry-After), sem circuit breaker (documentado); `TMDB_API_TOKEN` via env; métricas `tmdb_requests_total`/duração com labels fechados; hit/miss do cartaz; single-flight contra stampede |
| backend-dev | seguir com ressalvas | T1 ~6–8 h, T2 ~5–7 h, ambas < 30 arquivos; DELETE síncrono; single-flight fica p/ E5/E12; mantém `imdb_id` opcional; payload do evento em PT sem versionar |

## Debate (divergências e resolução)

1. **Invalidação do cache do cartaz** — security: evento na outbox × arquiteto/SRE/backend-dev: DELETE síncrono após o commit. → **Consenso (§1 anti-overengineering)**: DELETE síncrono no service após o commit; a janela de inconsistência (commit ok, delete falha) é limitada pelo TTL do cache e logada como `warn`. Evento só se houver múltiplas réplicas de API.
2. **Single-flight no cartaz** — SRE: entra agora × backend-dev: E5/E12. → **Consenso (§1 + medir antes)**: fica para quando o cartaz público do E5 existir e o k6 do E12 medir; registrado como exigência do E5.
3. **Payload do evento `catalogo.filme_criado` EN→PT sem versionar** — arquiteto perguntou. → **Consenso**: único consumidor é interno (projeção, idempotente) e não há produção; versionar = overengineering. O consumer passa a ler o payload novo na mesma task.
4. **`imdb_id`** → **Consenso**: mantém opcional, sem lógica em cima.
5. **PK** → **Consenso**: `GENERATED ALWAYS AS IDENTITY`, fim do `MAX(id)+1`.
6. **Filme sem duração no TMDB** → **Consenso** (qa + backend-dev, E3 depende): import rejeitado com erro explícito; operador cadastra manualmente.
7. **Token TMDB** → **Consenso**: v4 bearer em header.
8. **Pôster** — hotlink × cópia. → **Escalado** (decisão de produto): usuário escolheu **hotlink da CDN do TMDB** (URL validada: só `image.tmdb.org`).

## Conclusão

Escopo confirmado em **2 tasks**; nenhum ADR novo (ADRs 0002/0003/0005/0006 cobrem). Pendência do usuário (não bloqueia o desenvolvimento): criar conta TMDB e gerar o token v4 para uso real — o CI usa só o fake.

### Exigências por task

#### T1 — Migração `films`→`filmes` + CRUD do operador

- Migration única: tabela `filmes` em PT (`id BIGINT GENERATED ALWAYS AS IDENTITY`, `titulo`, `sinopse`, `duracao_min`, `ano`, `poster_url`, `imdb_id` opcional, `tmdb_id` opcional `UNIQUE`, `arquivado_em`, `criado_em`, `atualizado_em`), **dados preservados** (os 10 seeds), identity reposicionada após os dados; down reverte para `films`. up→down→up com dados no CI.
- CHECKs: título 1–255, duração 1–1440 min quando presente.
- Rotas públicas: `GET /filmes` (exclui arquivados), `GET /filmes/{id}` (404 se arquivado). Backoffice sob `Exigir(operador)`: `POST /backoffice/filmes`, `PUT /backoffice/filmes/{id}`, `POST /backoffice/filmes/{id}/arquivar`, `GET /backoffice/filmes` (inclui arquivados). Sem DELETE físico.
- Validação: título obrigatório ≤ 255, sinopse ≤ 2000, duração 1–1440, ano razoável; `poster_url`, se informado, só `https://image.tmdb.org/`.
- Criação manual e CLI `criar-filme` seguem emitindo `catalogo.filme_criado` (payload PT) na mesma TX; consumer/projeção atualizados; regressão E0a verde.
- Cache: DELETE síncrono da chave do cartaz após commit de criar/editar/arquivar; teste verifica a chave apagada no Redis real.
- Testes: matriz de autorização {sem token, cliente, operador, expirado} × rotas de backoffice; 2 inserts concorrentes sem colisão de id.

#### T2 — Import do TMDB

- Port `ImportadorFilme` no `catalogo`; adapter `internal/catalogo/tmdb` (stdlib `net/http`, host fixo `api.themoviedb.org`, bearer v4 via `TMDB_API_TOKEN`); fake `httptest` como 2º implementador.
- Timeout total 5 s; retry ≤ 3 com backoff+jitter só em 5xx/rede/timeout/429 (respeita `Retry-After`); **sem circuit breaker** (decisão da descoberta, documentada no PRD).
- `POST /backoffice/filmes/importar {tmdb_id}` (+ busca `GET /backoffice/tmdb/buscar?q=`): upsert por `tmdb_id` (ON CONFLICT), duração ausente → 422 com orientação de cadastro manual; pôster = URL da CDN validada; sinopse/título truncados/limitados e sem HTML; atribuição TMDB no corpo da resposta.
- Nunca logar token/URL com credencial. Métricas `tmdb_requisicoes_total{operacao,classe_status}` e duração; span filho.
- Testes: só contra httptest (grep impede domínio real na suíte); 429/5xx com assert do nº de tentativas; timeout; JSON malformado; idempotência sob concorrência; invalidação do cache.

### Exigências transferidas

- **E5**: single-flight/anti-stampede do cartaz + métricas hit/miss; SPA nunca usa `dangerouslySetInnerHTML` em título/sinopse; exibir a atribuição do TMDB.
- **E3**: FK `sessao.filme_id` → `filmes(id)`; filme arquivado não recebe sessão nova.

### Perguntas escaladas ao usuário (respondida em 2026-09-29)

1. Pôster → **hotlink da CDN do TMDB** (sem storage próprio).
