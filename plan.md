# plan.md — Plano da Task em Andamento

> Este arquivo é o plano vivo da task corrente **do projeto** — não confundir com o *plan mode* do Claude Code (que grava em `~/.claude/plans/`). Atualizado durante a implementação; reflete o estado real (regras em `roles.md` §6.11).

## Estado corrente (2026-10-01)

**Task 0041 — Dashboards de negócio, alertas e watchdog (E10 T2): implementada, em auditoria** (branch `feature/0041-dashboards-alertas`, sobre a 0040). Última task do E10.

### Plano da task 0041

1. ~~Dashboards~~ — 4 de negócio (funil; compensações/cancelamentos; reservas/ocupação; gateway/e-mail) só com métricas existentes.
2. ~~Alertas~~ — purga da trilha parada > 48 h; watchdog sempre ativo → contact point `heartbeat-externo` (healthchecks.io por env, rota própria, nunca ao Discord); textos de estorno cobrindo cancelamentos. 18 regras.
3. ~~Testes~~ — contrato dashboards/alertas × métricas do código (mutação conferida); stack com 7 dashboards, 3 datasources, 18 regras por uid e 2 contact points.
4. ~~Runbook~~ — `docs/observabilidade.md` (traces, alertas novos, healthchecks.io, pendências da E0c-CD).
5. Passe de julgamento → PR.

---

### Plano da task 0040

1. ~~App~~ — `otlptracehttp` v1.46.0; `exportacao.go` (lote sem bloquear, sem retry; `Sanitizar` remove query/URL/headers/parâmetros e troca o path pela rota); sampler `AlwaysSample` com coletor, cabeça 10% + descarte sem.
2. ~~Stack~~ — Tempo 2.10.4 (local, 72 h, sem porta); Alloy com receptor OTLP → memory_limiter → remoção de atributos → tail sampling (ERROR, > 300 ms, 10%) → Tempo, 512 MB; datasource Tempo + derived field `trace_id` no Loki; `app` com `OTEL_EXPORTER_OTLP_ENDPOINT` no compose de observabilidade.
3. ~~Testes~~ — coletor falso (sanitização), coletor fora (não bloqueia), `alloy validate`, Alloy→Tempo real (erro e lento chegam).
4. ~~Lint + govulncheck + passe de julgamento~~ — APROVADO, sem bloqueantes. Resolvido: IP do cliente e user agent também saem dos spans (LGPD) no app e no Alloy + teste. Registrados: eventos `exception.message` não sanitizados (erros não carregam PII hoje — premissa); restart do Alloy perde traces em decisão (aceito).

---

### Task 0039 — SPA: sessões e pedidos do operador: CONCLUÍDA e MERGEADA (PR #71). **E9 concluído.**

### Plano da task 0039

1. ~~API e fuso~~ — hooks de sessões/pedidos do operador; `paraUTCDoCinema` (UTC−3 fixo).
2. ~~Telas~~ — Sessões (programar; cancelar mostrando antes os pedidos pagos afetados) e Pedidos (filtros, detalhe com histórico, cancelar).
3. ~~Testes~~ — 6 de componente (13 no backoffice); E2E `backoffice.spec.ts` (sala → sessão → pública → compra D5 → operador cancela) + operador semeado no workflow de E2E.
4. ~~Lint + typecheck + build + passe de julgamento~~ — 1ª rodada REPROVADA (bloqueante: o E2E comprava D6, que é **vão** no layout modelo — timeout garantido no CI) → D5 na sala nova; `--profile app` no `exec` do seed. Revalidado o item reprovado → APROVADO. Registrados: extração da senha do seed pelo texto do `Printf` (protegida por `test -n`).

---

### Task 0038 — SPA: cancelar + backoffice de filmes e salas: CONCLUÍDA (PR #70)

### Plano da task 0038

1. ~~Tipos/API~~ — `cancelavel`, `useCancelarDaConta`, `cancelarPorConsulta`, `api.put`, `api/backoffice.ts`.
2. ~~Cancelar~~ — `ui/CancelarPedido` (confirmação + mensagens) na conta e na consulta (credenciais só em memória).
3. ~~Backoffice~~ — rota lazy `/backoffice/*` com guarda de papel; Filmes (TMDB, arquivar) e Salas (layout em JSON); link no menu do operador.
4. ~~Testes~~ — 3 de conta, 1 de consulta, 7 do backoffice (99 no total, verdes); E2E do cancelamento do convidado em `m4.spec.ts` (roda no CI).
5. ~~Lint + typecheck + build + passe de julgamento~~ — APROVADO, sem bloqueantes. Resolvido: foco do teclado na confirmação do cancelamento (vai a "Manter pedido" e volta ao botão) + teste; contagem do PRD. Registrados: testes de componente para 429/`nao_cancelavel`/`tmdb_sem_duracao`/`nome_em_uso` (mapeamento trivial); `window.confirm` no arquivar (acessível, ação rara).

---

### Task 0037 — trilha de auditoria + operador: CONCLUÍDA e MERGEADA (PR #69, `4403b0b`)

### Plano da task 0037

1. ~~Migration 017 + `internal/auditoria`~~ — trilha só com IDs (CHECKs), trigger contra UPDATE/TRUNCATE, `Registrar` na TX do chamador com o ator do context, purga em lotes + job diário no worker + métricas.
2. ~~Trilha nas mutações~~ — filmes (criar/editar/arquivar/importar), salas, sessões (criar/cancelar) — as que não tinham TX passam a abrir.
3. ~~Operador no pedido~~ — `GET /backoffice/pedidos` (filtros, e-mail mascarado, sem código), detalhe, `POST …/cancelar` (sem janela; 409 com a sessão iniciada; trilha na TX).
4. ~~Main + RBAC~~ — `comAtorDaTrilha`, rotas, purga; teste sobre todas as rotas `/backoffice/*` do main.
5. ~~Achado na revisão dos alertas~~ — cancelamento contava como compensação da saga (dispararia "Estorno automático executado" a cada cancelamento) → só estornos automáticos contam; alerta de purga parada e textos dos alertas de estorno → **E10** (a task passaria de 30 arquivos).
6. ~~Lint + suíte `-race` + passe de julgamento~~ — APROVADO, sem bloqueantes. Resolvidos: máscara de e-mail por runa (local curto todo oculto) + teste; ator ausente → 401 no `comAtorDaTrilha`. Registrados: `catalogo` usa `time.Now()` na trilha (sem relógio injetado — consistência futura); DELETE da trilha possível ao usuário único do banco (role só da purga no hardening E11).

---

### Task 0036 — cancelamento: CONCLUÍDA e MERGEADA (PR #68, `8ecc2a8`). E9 refinado (PR #67). (branch `feature/0036-cancelamento`, da main `7a1d87e`).

### Plano da task 0036

1. ~~Migration 016 + máquina de estados + janela~~ — motivos `cancelamento|operador|sessao_cancelada`, índice `pedidos(sessao_id)`; `pago + CancelamentoSolicitado → estorno_pendente`; `DentroDaJanela` inclusiva.
2. ~~Serviço e rotas~~ — `POST /pedidos/:id/cancelar` (conta) e `POST /pedidos/consulta/cancelar` (convidado, mesmo caminho/limite da consulta); `cancelavel` nas visões; métrica `cancelamentos_total{origem}`.
3. ~~Sessão~~ — `CancelarSessao` numa TX com a porta `PedidosDaSessao` (ligada no main), 409 `sessao_iniciada`, resposta `{"pedidos_estornados": n}`; pivô estorna pagamento de sessão cancelada.
4. ~~Testes~~ — 8 de integração (conta, convidado, recusas, ciclo completo, concorrência, sessão com estados mistos, pivô, corrida pivô × sessão) + unit da janela/matriz. **Desvios achados pelos testes:** (a) deadlock pivô × cancelamento da sessão (FK de `ingressos` pede KEY SHARE na sessão) → trava da sessão com `FOR NO KEY UPDATE`; (b) `LiberarDoPedido` não devolvia holds vendidos — o assento de pedido estornado nunca voltaria → passa a liberar `convertido` também (texto do ADR 0011 explicitado no #67; teste da reserva atualizado).
5. ~~Lint + suíte `-race` + passe de julgamento~~ — APROVADO, sem bloqueantes. Resolvidos: teste do ciclo isolado do lote global do job (estorno só do próprio pedido); `CancelarSessao` recusa rodar sem a porta do pedido; lista de arquivos do PRD. Registrados: cenários cliente × cancelamento da sessão e `tem_usado` concorrente (relevante quando o check-in existir). SPA do cancelamento → 0038.

---

### Task 0035 — SPA: consulta e ingresso + E2E do M4: CONCLUÍDA e MERGEADA (PR #66, `7a1d87e`)

### Plano da task 0035

1. ~~Rota de teste do e-mail~~ — `GET /__teste/emails?para=` no main (fake + `-mode=all` + travas da rota de pagamento) + unit.
2. ~~Telas~~ — `/consulta` (mensagem única), `/i/:ref` (válido/usado/404/410, QR `no-referrer`), links, `referrerPolicy: no-referrer` no cliente.
3. ~~CSP/axe~~ — `@axe-core/playwright` 4.13.0; CSP de produção no `vite preview` + gate de igualdade no `web-ci`; snippet `(ingresso)` do Caddy.
4. ~~E2E M4~~ — convidado e conta verdes localmente (5/5 com M3 e sessão), sob CSP e axe. **Achado do axe:** rodapé do TMDB com contraste 4,08:1 → `--nevoa` (corrigido).
5. ~~Lint + typecheck + CI + passe de julgamento~~ — APROVADO. Resolvidos: ordem do import `regexp` (gofmt) e formatação herdada do `stripe.go` (0033); gate da CSP por linha exata; roadmap (E8 ✅, M4 ✅). Registrados: assentos reservados por spec (M3 = C4, M4 = D3/D4) — documentar se novos specs comprarem; fake do e-mail sem teto em memória (só CI/dev).

---

### Task 0034 — Backend: consulta de convidado e página do ingresso: CONCLUÍDA e MERGEADA (PR #65, `2a76e67`)

### Plano da task 0034

1. ~~Queries~~ — `PedidoPorCodigo`, `IngressoParaPagina` (sqlc).
2. ~~Serviço/rotas~~ — `consulta.go` (e-mail em tempo constante contra fantasma; HMAC sempre calculado; 410 só após token válido), `POST /pedidos/consulta`, `GET /i/:ref`, `GET /i/:ref/qr.png`, headers, limitadores.
3. ~~Main~~ — porta `infoSessao` (sessao + catalogo), `notificacao.QR` injetado, redação do link no access log e no span.
4. ~~Testes~~ — 6 de integração (consulta, limites, ingresso com 10 variantes, revogado, versão do token) + redação (mutação conferida).
5. ~~Lint + suíte `-race` + CI + passe de julgamento~~ — APROVADO, sem bloqueantes. Resolvidos: versão de token sem segredo → HMAC com a v1 e 404 (antes 500 = oráculo de existência) + teste; godoc do `QR`. Registrados: lockout da consulta por e-mail conhecido (inerente ao teto por e-mail, aceito); 404 de rota não casada em `/i/a/b` sem os headers (sem token em jogo); limitador não atômico (padrão existente); teste de ordem dos middlewares no `setupRouter` (candidato); `RealIP` atrás do Caddy (E0c-CD).

---

### Task 0033 — SPA: checkout e pagamento: CONCLUÍDA e MERGEADA (PR #64, `93b49fd`)

### Plano da task 0033

1. ~~Deps~~ — `@stripe/stripe-js` 9.17.0 + `@stripe/react-stripe-js` 6.12.0 (Context7, npm audit, `lib.md`); `allow_redirects=never` no PaymentIntent (segredo nunca numa URL de retorno — decisão da implementação, dentro do RF de "client_secret só em memória").
2. ~~Telas~~ — checkout (`/sessoes/:id/pagamento`), meio por build (fake × Stripe, chunk do outro modo não gerado), acompanhamento `/pedido/:id` (backoff, teto 2 min), link no painel.
3. ~~Testes~~ — componentes (checkout, meios com mocks do Stripe, polling com timers falsos) + adapter Go; smoke manual do fluxo na stack local (assento → "Pagar (teste)" → pago → assento ocupado).
4. ~~CSP/gates~~ — Stripe na CSP do Caddy; `web-ci` barra teste/segredo no bundle; E2E builda em modo fake com a rota de teste ativa.
5. ~~Lint + typecheck + build + CI + passe de julgamento~~ — APROVADO, sem bloqueantes. Resolvido: `img-src https://*.stripe.com` na CSP (ícones do Payment Element). Registrados: smoke manual com `pk_test` olhando violações de CSP no E0c-CD (Caddy real); pedido criado antes do deploy e com retry do `CriarCobranca` depois pode receber `idempotency_error` do Stripe (parâmetros mudaram; janela de 24 h, raro — cai no 503 e o usuário refaz); o segredo vive no estado do checkout enquanto a rota está montada (memória de componente, aceito).

---

### Task 0032 — SPA: sessão, login/cadastro e "Meus pedidos": CONCLUÍDA e MERGEADA (PR #63, `f539bdf`)

### Plano da task 0032

1. ~~Proxy~~ — `cookiePathRewrite` no Vite (o cookie de refresh com `Path=/auth/refresh` nunca voltava pelo `/api` — achado na abertura; Caddy registrado para a E0c-CD).
2. ~~Sessão~~ — `sessao.ts` (token só em memória, `criarRefreshUnico` com Web Locks + releitura no lock, BroadcastChannel para resultado/saída) + `client.ts` (Autenticador injetado, Bearer, 1 retry após 401).
3. ~~Telas~~ — `/entrar`, `/cadastro`, `/conta/pedidos`, `/conta/pedidos/:id`, menu da conta, boot no `main.tsx`.
4. ~~Testes~~ — unit do refresh único e do módulo; componentes das telas; E2E de duas abas.
5. Lint + typecheck + build + E2E local (3/3, imagem oficial do Playwright) + CI + passe de julgamento — **auditoria APROVADA em 2026-09-30** (`security`, sem bloqueantes). Resolvidos: `/auth/*` sem Bearer imposto pelo caminho no cliente; contagem do PRD (22). Registrados: `BroadcastChannel` não fechado entre instâncias de teste (inofensivo); `sair()` com refresh em voo pode causar 1 refresh redundante (benigno); 2º refresh quando a mensagem do canal chega após o lock é seguro (cookie já rotacionado no jar).

---

### Task 0031 — Backend do checkout e da conta: CONCLUÍDA e MERGEADA (PR #62, `802e374`)

### Plano da task 0031

1. ~~Auth opcional~~ — `autenticacao.Opcional` (sem header → anônimo; inválido → 401; válido → contexto).
2. ~~Conta no pedido~~ — `usuario_id` só do JWT no `POST /pedidos`; `GET /pedidos/{id}` pelo carrinho ou pela conta; `GET /pedidos` ("Meus pedidos", paginado, migration 015).
3. ~~Retomada~~ — `Gateway.RecuperarSegredo` (Stripe Retrieve / fake) + `POST /pedidos/{id}/retomar` (dono, aguardando no prazo, `no-store`).
4. ~~Pagamento de teste~~ — `POST /__teste/pagar/{id}` só com gateway fake: evento assinado com o segredo do webhook → `Verificar` + pivô reais.
5. ~~Testes~~ — Opcional (5 casos), vínculo, meus pedidos, retomada, rota de teste presente/ausente, Stripe RecuperarSegredo.
6. ~~Lint + suíte + CI + passe de julgamento~~ — APROVADO, sem correções. Registrados: retomada só pelo carrinho (intencional); `PagarParaTeste` lê com `FOR UPDATE` fora de TX (inofensivo, rota só com fake).

---

### Refinamento E8 — CONCLUÍDO (PR #61, `6e9f880`)

---

### Task 0030 — E-mail de estorno: CONCLUÍDA e MERGEADA (PR #60, `11a4bdc`) — **E7 concluído**

### Plano da task 0030

1. ~~Evento~~ — `pedido.estornado` na mesma TX do CAS `estornado` (tarefas.go); porta `DadosParaAvisoDeEstorno` (só estornado; reutiliza a query da confirmação).
2. ~~Broker~~ — fila `notificacao.pedido_estornado` + DLX/DLQ próprias; DLQ em `FilasReplay` (replay + gauge).
3. ~~Notificação~~ — consumidor tipado (`Config.Tipo`), `AvisoDeEstorno` + template sem QR/link/token, chave `estorno-{pedido}`; segundo consumidor no worker com as mesmas métricas (`tipo=estorno`).
4. ~~Testes~~ — 1 evento por estorno, nenhum `pedido.confirmado` no pago-tarde, porta só p/ estornado, aviso sem ingresso.
5. ~~Suíte + lint + CI + passe de julgamento~~ — APROVADO. Resolvidos: comentário do consumidor; teste do tipo padrão (confirmação mede latência). Registrados: prova concorrente do CAS perdido no estorno e teste de integração da fila nova (garantidos por construção — mesma TX e lista fechada).

---

### Task 0029 — Provedor Resend: CONCLUÍDA e MERGEADA (PR #59, `da2dae1`)

### Plano da task 0029

1. ~~Adapter~~ — `notificacao.Resend` por HTTP cru (POST /emails, Bearer, Idempotency-Key, anexos inline base64 + content_id, timeout 5 s, resposta ≤ 64 KB); classes `ErrCotaEsgotada`/`ErrEnvioPermanente` pelo `name` do erro (Context7); erro nunca carrega o corpo.
2. ~~Consumidor~~ — resultado por entrega (ok/ignorado/transitorio/permanente/cota); cota e recusa → DLQ.
3. ~~Config/wiring~~ — `EMAIL_PROVEDOR` (fake proibido em produção — auditoria 0028 N2), `RESEND_API_KEY`, `EMAIL_REMETENTE`; métricas `morfeu_email_*` com `provedor`/`tipo` na allowlist; 2 alertas (16); gitleaks `re_`; `.env.example`.
4. ~~Testes~~ — adapter contra servidor falso (requisição + 8 classes + timeout + sem eco de PII), resultados do consumidor, config, labels.
5. ~~Lint + suíte + CI + passe de julgamento~~ — APROVADO. Resolvidos: `name` do erro do provedor filtrado (`[a-z_]{1,64}`); séries de recusa/cota nascem em 0; runbook do alerta cita chave revogada; contagem do PRD (17).

---

### Task 0028 — Notificação: núcleo: CONCLUÍDA e MERGEADA (PR #58, `1088c66`)

### Plano da task 0028

1. ~~Portas~~ — `pedido.DadosParaNotificacao` + `TokenIngresso` (HMAC `"ingresso:v1:"+id`, 43 chars); `sessao.DadosParaIngresso`; `catalogo.TituloDoFilme` (sem filtro de aberta/arquivado); adapter `fonteDoEmail` no main.
2. ~~E-mail~~ — `notificacao.Entregador` (carregar → montar → enviar), templates `html/template` + texto (Brasília, BRL), QR PNG inline por CID (skip2), fake do `EmailSender`; consumidor trata não notificável (ack) e inexistente (DLQ).
3. ~~Config~~ — `INGRESSO_TOKEN_SEGREDO_V1` (≥ 32 bytes; obrigatório em produção; dev gera aleatório) e `BASE_URL_PUBLICA` (https em produção).
4. ~~Testes~~ — QR decodificado (gozxing), golden, escape/sem imagem externa, entregador, consumidor, porta do pedido com PG real, config. **Bug achado pelos testes:** o `html/template` trocava `cid:` vindo de dado por `#ZgotmplZ` (QR não apareceria) → prefixo literal no template.
5. ~~Lint + suíte + CI + passe de julgamento~~ — APROVADO. Nota operacional no `ambiente-dev.md` (fixar o segredo do token no dev). **Transferido à 0029:** recusar o fake do e-mail em produção como critério explícito. Registrado: se o formato do código de assento mudar, sanitizar o `cid`.

---

### Refinamento E7 — CONCLUÍDO (PR #57, `43a84fd`)

---

### Task 0027 — Replay da DLQ + hardening do worker: CONCLUÍDA e MERGEADA (PR #56, `055dbf3`) — **E6 concluído**

### Plano da task 0027

1. ~~Replay~~ — `broker.Reprocessar` (lista fechada de DLQs → routing key de origem; get → publish com confirm → ack; `message_id`/headers de negócio preservados, sem `x-death`; dry-run devolve) + subcomando `replay-dlq` (padrão dos subcomandos existentes, em vez de `-mode=replay`).
2. ~~Worker~~ — panic no handler → DLQ (consumidor segue); gauge `morfeu_dlq_mensagens{fila}` para todas as DLQs; limpeza da outbox publicada > 7 d (lotes de 1000, 1 h); `stop_grace_period: 45s`. Prefetch mantido em 1 (decisão da 0005: semântica do `x-delivery-limit`).
3. ~~Cobranças abertas~~ — migration 014 (`cobranca_encerrada`); expiração lazy e reconciliação encerram a cobrança cancelada; varredura de expirados com cobrança aberta (aprovada → estorno tardio).
4. ~~Testes~~ — replay/panic/limpeza com RabbitMQ + PG reais; cobrança aberta paga; gauge multi-fila.
5. ~~Lint + suíte + CI + passe de julgamento~~ — APROVADO. Resolvidos no PR: replay confere a fila de origem antes do lote (publish sem rota seria descartado); panic loga tipo + stack, nunca o valor; teste do replay sem depender da ordem do requeue. **Pendência registrada (state.md):** backoff na varredura de cobranças abertas (um pedido que falha sempre pode ocupar o lote).

---

### Task 0026 — Observabilidade da saga: CONCLUÍDA e MERGEADA (PR #55, `cd139d7`)

### Plano da task 0026

1. ~~Broker~~ — `OccurredAt` na entrega/mensagem; fila `notificacao.pedido_confirmado` com DLX/DLQ próprias (a DLX de filmes é fanout).
2. ~~Notificação~~ — módulo `notificacao` (stub; `Entregar` injetável para o E7), consumidor no worker, histograma `checkout_confirmado_ate_notificado_segundos`.
3. ~~Métricas da saga~~ — `saga_compensacoes_total{passo}` (cobranca|estorno, só quando compensa de fato), `pedidos_presos{estado}`, funil `expirado`; `gateway_duration_seconds` sem `WithUnit` (bug da 0024: viraria `_seconds_seconds`).
4. ~~Alertas~~ — 5 regras (9 → 14) + teste da stack.
5. ~~Ponta a ponta~~ — `test/checkout` com PG/Redis/RabbitMQ reais: jornada da API até a notificação; notificação falhando → DLQ sem desfazer a venda. Stripe "já estornado" = sucesso.
6. ~~Lint + suíte~~ → **1º passe de julgamento REPROVADO** (`qa`): (1) a allowlist fechada de labels da telemetria descartava `passo`/`estado` (e `etapa`/`op` desde 0023/0024) — 3 alertas nunca disparariam; (2) corrida no teste ponta a ponta (lia o dedup antes do commit — o CI pegou). **Corrigido:** labels permitidas + teste do `/metrics`; espera pelo dedup commitado; DLQ conferida pelo `aggregate_id` e ≥ 3 tentativas; séries de compensação nascem em 0; buckets finos no histograma. Revalidação dos itens 1 e 7: **APROVADA**. Pendente para a 0027: alerta de profundidade da DLQ nova (`notificacao.pedido_confirmado.dlq`).

**Desvios registrados:** limpeza de pedidos abandonados → E11 (FK + retenção de 12 meses da trilha); varredura de `expirado` com cobrança aprovada → 0027.

---

### Task 0025 — Estorno + reconciliação: CONCLUÍDA e MERGEADA (PR #54, `b3a496f`)

### Plano da task 0025

1. ~~Borda~~ — `Gateway` com consultar/cancelar/estornar; Stripe (Retrieve/Cancel/Refunds com chave) via helper `chamar` (breaker + prazo + métricas); fake com estado por cobrança e falhas programadas.
2. ~~Dados~~ — migration 013 (índices parciais das varreduras); queries de vencidos, estornos pendentes e falha de estorno.
3. ~~Tarefas~~ — `Reconciliar` (aprovada → mesmo pivô; pendente → cancelar + expirar), `ExecutarEstornos` (chave `estorno-{pedido}`, backoff 1 min…1 h, erro a partir da 5ª), cancelamento na expiração lazy.
4. ~~Wiring~~ — worker monta reserva + pedido e roda as tarefas no WaitGroup do shutdown.
5. ~~Testes~~ — unit (adapter, fake, backoff) + integração (CA01–CA07), 2/2 com `-race`.
6. ~~Lint + CI + passe de julgamento~~ — APROVADO. Resolvidos no PR: loops param no shutdown (`ctx.Err()`), pedidos em backoff não ocupam o lote do estorno (lê 5× e processa 10), testes de consulta falha → adiado / sem cobrança → expira / contexto encerrado, nota operacional no `ambiente-dev.md` (fake em memória; worker exige as envs do Stripe). **Transferidos à 0026:** Stripe "charge already refunded" (chave de idempotência vencida após 24 h) tratado como sucesso; alerta/varredura de `expirado` cuja cobrança acabou aprovada (cancelamento lazy recusado + webhook perdido).

**Decisão de teste registrada:** as tarefas varrem a tabela inteira; os testes de tarefas começam encerrando pendências alheias (`semPendenciasAlheias`) — o pacote roda em sequência.

---

### Task 0024 — Stripe + webhook + pivô: CONCLUÍDA e MERGEADA (PR #53, `8cf6f6e`)

### Plano da task 0024

1. ~~Dependência~~ — `stripe-go/v86` v86.4.2 (Context7 `/stripe/stripe-go`, proxy.golang.org, OSV sem advisories) no `lib.md` antes do uso.
2. ~~Dados~~ — migration 012 `stripe_eventos`; queries de dedup, trava do pedido, estorno com motivo e emissão com `ON CONFLICT`; `DefinirCobranca` recupera cobrança órfã.
3. ~~Borda~~ — adapter Stripe (idempotência, timeout 5 s, breaker próprio com meio-aberto, métricas) e verificador do webhook (corpo bruto, 300 s, timestamp futuro recusado).
4. ~~Pivô~~ — `pivo.go`: dedup → `FOR UPDATE` → cruzamento → savepoint da emissão → `pago` + `pedido.confirmado` | `estorno_pendente` (divergencia/tardio/emissao).
5. ~~Wiring~~ — config com recusas de boot, gateway por config, webhook opcional, depguard, Stripe CLI no compose, regra `whsec_` no gitleaks.
6. ~~Testes~~ — unit (webhook, adapter contra servidor falso, breaker, config) + integração (CA01–CA10); verdes, webhook 2/2.
7. ~~Lint + CI + passe de julgamento~~ — APROVADO. Resolvido no PR: `MORFEU_GATEWAY=stripe` sem `STRIPE_WEBHOOK_SECRET` recusado no boot. **Transferidos à 0025:** o job de estorno libera os holds do pedido (divergência com pedido ainda aguardando mantém os holds presos até lá); meio-aberto do breaker deixa passar chamadas concorrentes (aceito no volume). **E0c-CD:** rever o teto do webhook por IP atrás do Caddy; `payment_failed` sem dedup só infla o funil (aceito).

**Desvio registrado:** o SDK não repete erro já respondido pela API (só rede e lock timeout) — adotada a política oficial do SDK em vez de "retry em 5xx" do refinamento; o 5xx conta no breaker.

---

### Task 0023 — Pedido: aggregate, máquina de estados e criação: CONCLUÍDA e MERGEADA (PR #52, `882d408`)

### Plano da task 0023

1. ~~Dados~~ — migration 011 (`pedidos` com índice "1 pendente por carrinho", `pedido_eventos` append-only, `ingressos` com índice da 2ª linha de defesa); queries com CAS e `ON CONFLICT DO NOTHING` na inserção; preço no mapa da `sessao` + porta `PrecoDaSessaoAberta`.
2. ~~Pagamento~~ — porta `pagamento.Gateway` + fake programável (falha na N-ésima, latência, registro).
3. ~~Domínio~~ — aggregate `Pedido` (total no servidor, código base32 80 bits), máquina por tabela, repositório com CAS + trilha, serviço (expiração lazy em TX própria → abertura em TX → cobrança fora da TX → desfazer em falha), handler.
4. ~~Wiring~~ — adapter `reservaDoPedido` no `main`, limitadores 10/5 por min, `checkout_funil_total{etapa}`, depguard `pedido-domain`/`pedido-pagamento`.
5. ~~Testes~~ — unit (matriz completa, validação) + integração (CA01–CA12), 3/3 com `-race`.
6. ~~Lint + CI + passe de julgamento~~ — APROVADO. Não-bloqueantes resolvidos no PR: contexto desacoplado (5 s) para gravar a cobrança/desfazer; `Cache-Control: no-store` com `client_secret`. **Transferidos:** 0024 — recusar o fake em produção; 0024/0025 — resolver pedido também por `metadata.pedido_id` e cancelar cobrança órfã (quando `definirCobranca` falha após o gateway); E0c-CD — confirmar proxy confiável para `RealIP` (X-Forwarded-For).

**Decisão registrada (RN03):** o pedido fixa o prazo do hold (`expira_em + 2 min`) mesmo que encurte uma extensão do cliente — pendência da auditoria 0022.

---

### Task 0022 — Reserva: holds vendidos ocupam o assento + portas transacionais: CONCLUÍDA e MERGEADA (PR #51, `ad2e1b0`)

### Plano da task 0022

1. ~~Migration 010~~ — coluna `pedido_id` + índice único parcial `holds_assento_ocupado` (`ativo` + `convertido`) + índice por pedido; down documentado.
2. ~~Queries~~ — trava com o mesmo predicado do índice e roubo só de `ativo` vencido (zera `pedido_id`); ocupação conta `convertido`; estender/liberar do cliente só sem pedido; `PrenderParaPedido`/`ConverterDoPedido`/`ConvertidosDoPedido`/`LiberarDoPedido`.
3. ~~Portas~~ — `pedido.go` com os 3 métodos que recebem `outbox.Tx` (únicos); `Hold.EmPedido` + 409 `hold_em_pedido`; `Dono.Hash`/`DonoDoHash`; métrica `reserva_holds_convertidos_total`.
4. ~~Testes~~ — `pedido_test.go` (CA01–CA08: vendido sob corrida de 20, preso não roubável até o prazo, cobertura, idempotência, liberar); suíte do E4 intacta; 3/3 com `-race`.
5. ~~Lint + CI + passe de julgamento~~ — APROVADO. Não-bloqueantes: texto do RNF02 e `sqlc.yaml` no PRD (corrigidos); **transferidos à 0023**: teste de que o hold preso conta no teto de 6, ocupação após `LiberarDoPedido`, e a regra de `ate` (a 0022 fixa exatamente o prazo do pedido — pode encurtar um hold estendido; a 0023 confirma ou troca por `GREATEST`).

---

### Task 0021 — E2E do M3: CONCLUÍDA e MERGEADA (PR #49, `38aa433`)

### Plano da task 0021

1. ~~Playwright~~ — `@playwright/test` 1.63.0 (lib.md); `playwright.config.ts` (Chromium, `baseURL :4173`, `webServer` = build + `vite preview` com o proxy `/api`, trace em falha, `expect` 15 s = polling 4 s + cache 3 s); Vitest restrito a `src/**/*.test.*`.
2. ~~Jornadas~~ — Page Object `PaginaSessao` (role/nome acessível); **M3** com 2 contextos (B seleciona C4 → A reserva → B recebe a recusa nomeada e vê "ocupado" → A libera → B vê "livre" pelo polling → B reserva → limpeza); caminho feliz cartaz → filme → sessão; `seed.sql` com sala por timestamp (execuções repetidas não colidem) e sessão descoberta pela API.
3. ~~CI~~ — `e2e.yml` (ARM64, SHAs pinados, env do compose gerado com `JWT_SEGREDO` aleatório, stack pelo compose, espera `/health`, seed, `playwright install --with-deps chromium`, relatório + logs em falha, `down -v` sempre); caminhos duplicados em vez de âncora YAML.
4. ~~Relay~~ — `TestRelay_BrokerIndisponivelNaoCrashaEReentrega` para o broker **antes** de enfileirar + `t.Cleanup` que religa; pacote `outbox` `-race` verde e o trio afetado 3/3.
5. ~~Auditoria 0020~~ — `aoVencer` com `useCallback`; teste do hold vencido com **exatamente 1** reconsulta (38 testes verdes).
6. ~~Docs~~ — `ambiente-dev.md` (E2E local/imagem), roadmap (E5 ✅ M3), skill `e2e-testing` reativada (roles.md §4.4).

---

### Task 0020 — SPA: mapa integrado à trava: CONCLUÍDA e MERGEADA (PR #48, `f355cd8`)

Auditoria APROVADA (2026-09-29, `qa`); não-bloqueantes resolvidos na 0021 (`aoVencer` estável + contagem exata; CAs marcados).

#### Plano executado da 0020


1. ~~Dados~~ — tipos `Hold`/`Ocupacao`; `useMapa` (`staleTime: Infinity`), `useOcupacao` (`refetchInterval` 4 s, `refetchIntervalInBackground: false`, só depois do mapa), `useMeusHolds`; mutations travar/estender/liberar que **sempre** (sucesso ou erro) invalidam holds + ocupação — o 409 atualiza o mapa na hora.
2. ~~Página da sessão~~ — mapa + legenda + "Reservar N assentos" (desabilitado sem seleção/durante envio; o assento só vira "seu" depois do 201 + `GET /holds`); alternar um "seu" libera; mensagens de 409 com os assentos perdidos (saem da seleção), `limite_holds`, 429, genérica; selecionado tomado no polling sai da seleção com aviso (ajuste de estado na renderização, sem effect); 404 → sessão indisponível.
3. ~~Painel "Seus assentos"~~ — contagem mm:ss por `expira_em` (`useAgora` de 1 s só com holds), aviso ≤ 60 s, "Mais 10 minutos" uma vez, "Liberar"; vencido → reconsulta.
4. ~~Auditoria 0019~~ — letra da fileira como `role="rowheader"` (a da direita saiu); `web-ci` com "demo fora do bundle de produção".
5. ~~Testes~~ — 38 verdes (3/3 execuções): polling 4 s + pausa sem foco (`focusManager`) + mapa 1×, reserva sem otimismo, 409 com recarga imediata, limite/429, tomado no polling, painel (contagem, aviso, extensão única, liberar), vencido some; lint/typecheck 0; build 97,6 KB gzip; audit 0. Conferência visual pendente da imagem do Playwright (pull lento; Chrome ausente e libs nativas faltando p/ o headless shell).

---

### Task 0019 — SPA: mapa de assentos isolado: CONCLUÍDA e MERGEADA (PR #47, `0117c41`)

Auditoria APROVADA (2026-09-29, `qa`); não-bloqueantes resolvidos na 0020 (rowheader, verificação do bundle no CI, checkboxes marcados no fechamento).

#### Plano executado da 0019


1. ~~Estado puro~~ — `estado.ts`: precedência meu > selecionado > ocupado > bloqueado > livre (o hold próprio também vem na ocupação pública), `alternavel`, `rotuloDoAssento` ("Fileira C, assento 7, PCD, ocupado").
2. ~~Componente~~ — `MapaDeAssentos` puro (sem I/O): "Tela", `role=grid`/`row`/`gridcell`, vão = célula vazia sem botão, roving tabindex (1 tab stop), setas pulando vãos, ↑/↓ para a mesma coluna ou o assento mais próximo, Home/End, Enter/Espaço; `aria-pressed` (selecionado/meu), `aria-disabled` (ocupado/limite — continua focável), marcas ✓ ● × ♿ redundantes à cor; limite de 6 (selecionados + meus) com aviso `aria-live`; `Legenda`.
3. ~~Fixture + demo~~ — sala 6 × 10 com corredor e fileira curta; `/_demo/mapa` só com `import.meta.env.DEV` (import dinâmico) — **ausente do bundle de produção** (grep no `dist`).
4. ~~Testes~~ — 30 verdes (estado 3, mapa 5 com user-event: grade, teclado completo, Enter/Espaço, ocupado não alterna, aria-pressed, limite); lint/typecheck 0; build 94 KB gzip; audit 0. Conferência visual (0018 + 0019) antes do merge — imagem do Playwright baixando.

---

### Task 0018 — SPA: cartaz e sessões do filme: CONCLUÍDA e MERGEADA (PR #46, `849064b`)

Auditoria APROVADA (2026-09-29, `qa`); não-bloqueante: a UI assume sessões ordenadas por `inicio` (contrato RN01 da API).

#### Plano executado da 0018


1. ~~Dados~~ — `tipos.ts` (contrato público), `consultas.ts` (`useFilmes`/`useFilme`/`useSessoesDoFilme`; política de retry **global** no `QueryClient`: só rede/5xx, 1× — por consulta ela sobrescrevia o `retry: false` dos testes).
2. ~~Formatação~~ — `formato.ts` sempre em `America/Sao_Paulo`/pt-BR (hora, dia, chave do dia, BRL, duração), testada com instantes que viram o dia.
3. ~~UI base~~ — `Poster` (img só de `https://image.tmdb.org`, senão tipográfico com gradiente por **classe** — CSP `style-src 'self'` proíbe style inline), `Estado` (carregando/erro com "Tentar de novo"/vazio).
4. ~~Telas~~ — Cartaz (grade 4→2→1, cartão-link, meta mono) e Página do filme (pôster grande, sinopse como texto, sessões agrupadas pelo dia local em chips-link para `/sessoes/:id`, 404 → "não está em cartaz", id inválido sem chamada); shell com topbar/rodapé em CSS Modules e rota placeholder de sessão.
5. ~~Testes~~ — 22 verdes (formato 5, cartaz 4, filme 5 incl. sinopse hostil como texto, shell 3, cliente 5); lint/typecheck 0; build 94 KB gzip; audit 0. Conferência visual pendente (Chrome ausente p/ o Playwright MCP; imagem Docker do Playwright baixando — será usada no E2E da 0020).

---

### Task 0017 — SPA: fundação: CONCLUÍDA e MERGEADA (PR #45, `cce783c`)

Auditoria APROVADA (2026-09-29, `security`); informativos: HSTS comentado até o TLS do E0c-CD; `object-src` coberto por `default-src`. Primeiro `web-ci` verde em 16 s (ARM64).

#### Plano executado da 0017


1. ~~Dependências~~ — versões do registro npm e peers conferidos: **TypeScript 6.0.3** (7.x incompatível com typescript-eslint `<6.1`), **ESLint 10 sem eslint-plugin-react** (peer `^9.7` → `react/no-danger` virou `no-restricted-syntax`); `lib.md` antes do install; `npm audit` 0 vulnerabilidades.
2. ~~Scaffold~~ — `web/` com versões exatas, TS estrito (`noUncheckedIndexedAccess`, `verbatimModuleSyntax`), Vite 8 com proxy `/api` + `rewrite` (dev e preview), Vitest (jsdom), ESLint flat type-checked + react-hooks + proibições (`dangerouslySetInnerHTML`, `localStorage`/`sessionStorage`, `fetch` fora de `src/api/`) — as 3 verificadas com arquivo temporário. `MORFEU_API` removido (evitaria `@types/node`).
3. ~~Identidade~~ — `tokens.css` (7 cores + ClassInd, tipografia, raios, halo tungstênio, reduced-motion); 4 woff2 variáveis **com SHA-256 idêntico ao `fonts.gstatic.com` e ao protótipo** + OFL das 3 famílias.
4. ~~Código~~ — `api/client.ts` (base `/api`, `credentials: 'include'`, `X-Requested-With` só nas escritas, `ErroApi` com código/corpo, rede → status 0) com 5 testes; shell (`BrowserRouter` + `QueryClientProvider`, cartaz placeholder, 404 amigável, atribuição TMDB) com 2 testes.
5. ~~CI/docs~~ — `web-ci.yml` (ARM64, setup-node v7 pinado por SHA, `npm ci` → lint → typecheck → test → build → `npm audit --audit-level=high`); `configs/caddy/seguranca.caddy` (CSP + nosniff + Referrer-Policy, HSTS comentado até o TLS); `ambiente-dev.md` (seção do front); pendência das fontes fechada no doc de identidade.
6. ~~Verificação do proxy (CA05)~~ — API real (`-mode=api`, PG/Redis efêmeros em portas livres — 5432/5672 ocupadas por outro projeto) + `vite`: `/api/filmes` idêntico a `/filmes`; trava via proxy → 201 com `Set-Cookie ... Path=/; HttpOnly; Secure; SameSite=Strict`; `GET /api/holds` com o cookie devolve o hold; ocupação reflete. Ambiente derrubado.

Gate local: lint 0, typecheck 0, 7 testes verdes, build 89 KB gzip, audit 0.

---

### Task 0016 — Reserva: ocupação pública, cache e alertas: CONCLUÍDA e MERGEADA (PR #42, `b9ad75a`)

Auditoria APROVADA (2026-09-29, `qa`), sem não-bloqueantes novos.

#### Plano executado da 0016

1. ~~Query~~ — `OcupadosDaSessao` (só códigos de holds vivos, pelo índice único parcial).
2. ~~Serviço/rota~~ — `ocupacao.go`: cache `reserva:ocupacao:{id}` TTL 3 s sem invalidação; no hit nem a porta é consultada; no miss, sessão indisponível → 404 e nada é cacheado; falha do cache cai no banco. `GET /sessoes/:id/ocupacao` com `Cache-Control: public, max-age=2`. `Config.Cache` injetado no `main`; depguard `reserva-domain` + `internal/cache`.
3. ~~Alertas~~ — "Recusas de trava anormais" (> 60 em 5 min) e "Sweeper de holds parado" (sem passada em 10 min com a API no ar); teste da stack: 9 regras.
4. ~~Testes~~ — contrato só `sessao_id`+`ocupados`; liberado e vencido não contam; TTL no Redis em (0, 3 s]; hold novo invisível enquanto o cache vale e visível após a expiração da chave; inexistente/cancelada/iniciada → 404 sem cachear; `Cache-Control`. Suíte completa `-race` verde (inclui a stack de observabilidade); lint 0 issues; `sqlc diff` limpo.

---

### Task 0015 — Reserva: trava de assento + sweeper: CONCLUÍDA e MERGEADA (PR #41, `36613c3`)

Auditoria APROVADA (2026-09-29, `security`); informativo: a chave do advisory lock por dono usa 32 bits do hash (colisão só serializa dois donos — inofensivo).

### Plano da task 0015

1. ~~Migration 009~~ — `holds` com `UNIQUE (sessao_id, assento_codigo) WHERE status='ativo'` + índices parciais de expiração e dono; CHECKs de formato, hash de 32 bytes, status e extensões.
2. ~~Queries/sqlc~~ — bloco próprio só com a 009 (FK resolvida sem ler `sessoes`); upsert `TravarAssento` com `DO UPDATE ... WHERE holds.expires_at <= @agora` (sem linha = 409); advisory lock por dono/sweeper; extensão e liberação com guarda; `ExpirarVencidos` em lote com `SKIP LOCKED`.
3. ~~Módulo `reserva`~~ — VOs `AssentoCodigo`/`StatusHold`, aggregate `Hold` (`Vivo`, `Estender`), `Lote` (1–6, sem repetição, ordenado, contido no layout); `Dono` (SHA-256 do token de carrinho de 32 bytes); repository manual; serviço (rate limit, trava tudo-ou-nada com teto sob advisory lock, idempotência do próprio assento, retry em 40P01, métricas pós-commit); `Sweeper` próprio para o worker; handler com cookie HttpOnly/Secure/Strict + `X-Requested-With`, 404 para hold alheio.
4. ~~Wiring~~ — porta `sessao.AssentosDaSessaoAberta`; `montarReserva` (limitadores `trava-ip` 30/min e `trava-dono` 20/min, 4 contadores sem label); sweeper no `startWorker`; depguard `reserva-domain`.
5. ~~Testes~~ — unit (código, lote, borda exata do prazo, extensão única, token); integração PG+Redis reais com relógio injetado: corrida canônica 20 donos × 10 rodadas + invariante, roubo de vencido (inclusive sob corrida de 20), lote tudo-ou-nada, lotes cruzados 10 rodadas sem 500, teto 6 (sequencial e paralelo), extensão/liberação/posse → 404, CSRF 403, cookie, hash no banco, 429 na 31ª, porta (inexistente/cancelada/iniciada → 404; vão/fora da grade → 400), sweeper + corrida sweeper × roubo. Módulo 3× seguidas verde com `-race`; suíte completa `-race` verde; lint 0 issues; `sqlc diff` limpo.

---

### Task 0014 — Sessões: leitura pública, mapa e cache: CONCLUÍDA e MERGEADA (PR #39, `fea38b1`)

Auditoria APROVADA (2026-09-29); não-bloqueante: arquivar filme não invalida o cache de sessões públicas (defasagem ≤ 60 s) → E5.

### Plano da task 0014

1. ~~Queries~~ — `ListarSessoesFuturasDoFilme` (JOIN salas, índice parcial da 008), `BuscarMapaDaSessao`, `ListarSessoesBackoffice` com `sqlc.narg` (sala, [desde, ate)), `CancelarSessao` devolvendo `filme_id`.
2. ~~Service~~ — `ListarSessoesPublicas` (cache `sessao:filme:{id}:futuras` TTL 60 s; o que vem do cache é refiltrado por `inicio > agora`), `Mapa` (layout + `Assentos()`), `invalidarFilme` em criar/cancelar, `FiltroSessoes`, `mesmoLayout` normalizado (ordena vaos/pcd — auditoria 0013).
3. ~~Handler/wiring~~ — `GET /filmes/:id/sessoes` (Cache-Control 30 s) e `GET /sessoes/:id/mapa` públicos, registrados em todos os modos; filtros do backoffice com 400 para inválidos; `montarSessao` recebe o cache; depguard `sessao-domain` + `internal/cache`.
4. ~~Testes~~ — unit `mesmoLayout` (ordem ignorada, diferença real detectada, original intacto); integração (PG + Redis): lista pública em ordem sem campos de filme, cache populado/invalidado em cancelar e criar, **sessão que começa durante o TTL some da resposta servida do cache** (relógio injetado), filme sem sessões `[]`; mapa 5×8 − vão = 39 assentos com PCD, 404 p/ iniciada/cancelada/inexistente; filtros por sala/dia/combinados e 3 inválidos → 400; layout reordenado aceito com sessões futuras. Suíte completa `-race` verde; lint 0 issues; `sqlc diff` limpo.

---

### Task 0013 — Sessões e salas (escrita): CONCLUÍDA e MERGEADA (PR #38, `c5478a2`)

Auditoria APROVADA (2026-09-29, `qa`); não-bloqueantes: tipo `Filme` gerado sem uso no `sessao/db` (efeito do schema 001/007 p/ resolver a FK — aceito); `mesmoLayout` sensível à ordem (**corrigido na 0014**); `UPDATE` direto no seed num teste (frágil a reordenação — registrado). **CI do PR revelou 500** em 1 de 2 criações concorrentes conflitantes (runner ARM64; não reproduziu em 75 corridas locais) → hipótese deadlock 40P01 da EXCLUDE: serviço repete o INSERT só em 40P01 (até 3×), erro inesperado loga SQLSTATE, teste roda 20 rodadas com logs via zaptest. CI verde depois; o caminho de retry não apareceu no log (zaptest só imprime em falha) — **diagnóstico não confirmado**, registrado.


1. ~~PRD 0013~~ — lista fechada de 29 arquivos (fechamento de status da 0012 adiado p/ a 0014).
2. ~~Migration 008~~ — btree_gist; `salas` (nome único, layout jsonb); `sessoes` (FK filmes/salas, snapshot `duracao_min`, `fim`, `preco_centavos` 100–100000, status) com **`EXCLUDE USING gist (sala_id =, tstzrange(inicio,fim,'[)') &&) WHERE status='agendada'`**; índices `sala_id` e `(filme_id, inicio)` parcial. Validada à mão (encostada às 12:00 aceita; sobreposição → 23P01; up/down/up).
3. ~~Módulo `sessao`~~ — `layout.go` (schema estrito, ≤64 KB, 26×50, sem repetição, PCD fora de vão, `Assentos()` determinístico sem vãos); `service.go` (validação na ordem do PRD, porta `FonteFilmes`, `CalcularFim` = início + duração + 20 min, conflito decidido pelo banco → reconsulta do conflitante → 409, layout imutável com sessão futura, cancelamento idempotente, log com `operador_id`); `handler.go` (backoffice com middleware e `operadorDe` injetados, JSON estrito); `db/erros.go` (único com `pgconn`: `EhConflitoDeHorario`/`EhNomeDuplicado`).
4. ~~Wiring~~ — `catalogo.Servico.DuracaoFilmeAtivo` (porta); `montarSessao` no main (contador `sessao_conflitos_total` via callback — domínio sem OTel); depguard `sessao-domain` strict.
5. ~~Testes~~ — unit (15 casos de layout, códigos determinísticos, `CalcularFim` inclusive virada do dia, validação antes do banco); integração com PG real e catálogo real como porta: bordas (parcial início/fim, contida → 409 com conflitante; encostada → 201; outra sala → 201; cancelar libera e é idempotente), **5 rodadas de 2 criações concorrentes → sempre 1×201 + 1×409**, filme arquivado/inexistente/sala inexistente → 422, passado/preço/RFC3339/campo desconhecido → 400, imutabilidade do layout (409) vs nome editável, nome duplicado 409, matriz 6 rotas × {sem token, cliente}. Um oráculo de teste corrigido (a sessão "encostada" ocupava o horário usado no teste de "cancelar libera"). Suíte completa `-race` verde; lint 0 issues; `sqlc diff` limpo.

---

### Task 0012 — Importação do TMDB: CONCLUÍDA e MERGEADA (PR #37, `1514361`)

Auditoria APROVADA (2026-09-29, `security`) — achados só informativos: termo de busca aparece no log de request (não sensível); remoção de HTML por regex é defesa em profundidade (SPA escapa); sem rate limit dedicado nas rotas de TMDB (uso só do operador).


1. ~~PRD 0012~~ — Context7 `/websites/developer_themoviedb_reference`.
2. ~~Catálogo~~ — port `FonteTMDB` (setter `ComFonteTMDB`, evita mexer em todos os chamadores de `NovoServico`); `ImportarDoTMDB` (upsert `ON CONFLICT (tmdb_id)` com `xmax = 0` → evento só na criação; reimport atualiza sem desarquivar; 422 sem duração); `BuscarNoTMDB`; normalização (HTML removido, truncagem, pôster montado por nós só de `poster_path` com formato válido, imdb validado, ano da data); atribuição TMDB nas respostas.
3. ~~Adapter `internal/catalogo/tmdb`~~ — stdlib, host fixo, bearer, 5 s/tentativa, ≤ 3 tentativas (5xx/rede/429 c/ Retry-After ≤ 5 s; 404/4xx sem retry), corpo ≤ 1 MiB, erros sem token, métricas `tmdb_requisicoes_total{operacao,classe_status}` + duração, span `tmdb.<operacao>`; sem circuit breaker.
4. ~~Wiring~~ — `TMDB_API_TOKEN` opcional (`conectarTMDB`; ausente → 503 `tmdb_nao_configurado`); allowlist `operacao`/`classe_status`; env examples; lib.md (TMDB implementado).
5. ~~Testes~~ — adapter com httptest (tentativas por classe, Retry-After e teto, timeout, JSON malformado, header, token fora dos erros, busca com escape e limite 20), guarda-chuva contra o host real em qualquer `_test.go`; normalização hostil (paths `../`, `//host`, query, svg; duração 5000; imdb `javascript:`); integração com fonte fake (2º implementador — o adapter não pode ser importado no pacote por ciclo): cria 201/reimporta 200/1 evento/não desarquiva/cache invalidado, 422/404/503/400, concorrência → 1 filme e 1 evento (um 201 + um 200), matriz. Suíte completa `-race` verde; lint 0 issues; `sqlc diff` limpo.

---

### Task 0011 — Catálogo: CONCLUÍDA e MERGEADA (PR #36, `dfe0d01`)


1. ~~PRD 0011~~ — lista fechada (27 arquivos reais; fechamento de status da 0010 adiado p/ a 0012).
2. ~~Migration 007~~ — renames PT, `criado_em`→TIMESTAMPTZ, `tmdb_id` UNIQUE, `arquivado_em`, `atualizado_em`, CHECKs (título, duração 1–1440), `GENERATED ALWAYS AS IDENTITY` + `setval` acima do maior id; down completo. Validada à mão (up → próximo id 11; down preserva os 11; up → 12) e pelos testes.
3. ~~Catálogo~~ — `Servico` (ListarPublicos c/ cache read-through `catalogo:filmes:publicos`, BuscarPublico, ListarBackoffice, Criar c/ evento na mesma TX, Atualizar, Arquivar idempotente, validação RF04, `invalidarCartaz` síncrono pós-commit); `Handler` público + backoffice com middleware injetado (catálogo não importa `autenticacao`); projeção com payload PT; `cache.Delete`; overrides timestamptz→time.Time.
4. ~~Wiring~~ — `registrarRotasDeDominio` (cartaz sempre; /auth + backoffice só em api|all); CLI `criar-filme` exige `-duracao` (mesmo caso de uso).
5. ~~Testes~~ — harness de `internal/integration*`, `outbox` e `cmd/morfeu` pelos arquivos de migration (fim do DDL duplicado); assert de ordem do cartaz virou presença (ordem agora definida: criado_em/id DESC); novos: validação por campo (12 casos, inclui host-sufixo `image.tmdb.org.evil`), cartaz PT/404, matriz 4 papéis × 4 rotas, CRUD/arquivamento idempotente, 8 criações concorrentes sem colisão + 8 eventos, invalidação verificada no Redis. Suíte completa `-race` verde; lint 0 issues; `sqlc diff` limpo.
6. **Auditoria APROVADA (2026-09-29, `qa`)** — PR #36. Não-bloqueante aplicado: `TestMigrationsUpDownUpIdempotent` passa a executar os arquivos de down (007 → verifica os 10 filmes em films/EN → 001) em vez de `DROP TABLE`. Registrado: assert de ordem do cartaz ficou por presença (robustez).

---

### Task 0010 — Refresh + pseudonimização: CONCLUÍDA e MERGEADA (PR #35, `5d2ca7f`)


1. ~~PRD 0010~~ — desvio registrado: logout em `/auth/refresh/logout` (cookie com `Path=/auth/refresh`).
2. ~~Migration 006 + queries~~ — `refresh_token` (hash BYTEA UNIQUE, índices usuario/familia/expira); `TravarRefreshPorHash` com `FOR UPDATE OF r` + JOIN do papel; revogação por família/hash/usuário; `PseudonimizarUsuario` (só papel cliente); overrides `timestamptz`→`time.Time`/`*time.Time` (domínio sem pgtype).
3. ~~`sessao.go`~~ — refresh 32 B base64url + SHA-256; `Renovar` numa TX (`outbox.WithTx`): reuso revoga a família e **commita** (erro devolvido depois), métrica + log `refresh_reuso_detectado` só com `familia_id`; `Encerrar` idempotente; `RemoverConta` (pseudonimiza + revoga tudo); `LimparRefreshExpirados`.
4. ~~Handler/wiring~~ — login seta o cookie (`HttpOnly; Secure; SameSite=Strict; Path=/auth/refresh; Max-Age=604800`); `/auth/refresh` e `/auth/refresh/logout` exigem `X-Requested-With: morfeu`; `DELETE /auth/conta` só cliente; limpeza horária no worker (`startWorker`); depguard `identidade-domain` + `internal/outbox`; 7ª regra de alerta (reuso).
5. ~~Testes~~ — cookie + hash no banco; CSRF 403 sem consumir o token; rotação na mesma família; reuso revoga inclusive o sucessor (2 logs: usado + revogado); **corrida 20× com barreira sob `-race`: sempre 1 sucesso + família revogada**; logout; pseudonimização com oráculo exato + e-mail liberado + operador 403; limpeza. Ajustes de teste: lista de rotas POST permitidas, IP por cadastro (o limite de 10/h da 0009 é real). Suíte completa `-race` verde; lint 0 issues; `sqlc diff` limpo.
6. **Auditoria APROVADA (2026-09-29, `security`)** — PR #35. Não-bloqueante aplicado: nota de triagem no alerta de reuso e no runbook (falso positivo multi-aba até o single-flight do E8). Registrados: canal de timing teórico via e-mail tombstone (UUID não adivinhável).

---

### Task 0009 — Identidade: CONCLUÍDA e MERGEADA (PR #34, `3ef3188`)


1. ~~PRD 0009~~ — `docs/prd/0009-identidade-registro-login.md`; `x/crypto` direta **v0.55.0** (a 0.56.0 exige Go 1.26 e subiria o toolchain — revertido ao notar o bump do `go` no go.mod).
2. ~~Migration 005 + sqlc~~ — `usuario` (UNIQUE + CHECK minúsculo + CHECK papel); queries `:execrows` (ON CONFLICT) e `:many`+LIMIT 1 — domínio sem `pgx.ErrNoRows`; override uuid→`google/uuid`.
3. ~~Módulo~~ — `senha.go` (Argon2id PHC, parâmetros lidos do hash, comparação constante, senha aleatória sem viés), `service.go` (registro c/ limite por IP + semáforo; login: limitadores conta/IP → semáforo → hash real **ou dummy** → falha idêntica; `SeedOperador` como função de pacote — CLI não precisa de emissor/limitadores), `handler.go` (`/auth/registro|login|eu`, BodyLimit 16K, `Cache-Control: no-store`, JSON malformado sem eco), `errors.go`.
4. ~~Wiring~~ — `montarIdentidade` (só em api|all; `ValidarAutenticacao` fatal sem `JWT_SEGREDO` ≥ 32 B), `IPExtractor` direto, limitadores `morfeu:auth:{conta,ip,registro}:`, CLI `seed-operador`; config Argon2/HASH_CONCORRENCIA com faixas; smoke do CI com segredo efêmero; env examples; depguard `identidade-domain` (strict) + `identidade-isolada`.
5. ~~Testes~~ — unit (PHC, malformados, salts, senha aleatória, validação) + integração PG/Redis pelas rotas: registro (papel ignorado, duplicado por caixa, 400 por campo), login + `/auth/eu`, **contrato anti-enumeração** (corpo/headers idênticos + hash nos 2 ramos por contador), limites conta/IP/reset, semáforo, seed idempotente + nenhuma rota cria operador, senha/e-mail nunca nos logs. Suíte completa `-race` verde; golangci-lint 0 issues; `sqlc diff` limpo.
6. **Auditoria APROVADA (2026-09-29, `security`)** — PR #34. Não-bloqueante nº 1 aplicado: teto nos parâmetros lidos do hash armazenado (m ≤ 1 GiB, t ≤ 10, p ≤ 16 — espelha a config) contra DoS por hash adulterado + 3 casos no teste. Demais: govulncheck do CI confirma x/crypto; boot sem Redis segue o fallback da 0008.

---

### Task 0008 — Autenticação (plataforma): EM PR #32 (auditoria APROVADA após correção; CI verde; merge aguardando autorização)


Divisão da T1 (§6.3 — ~34 arquivos): **0008** plataforma → **0009** `identidade` → **0010** T2.

1. ~~PRD 0008~~ — `docs/prd/0008-autenticacao-plataforma.md`; `golang-jwt/jwt/v5` v5.3.1 no lib.md (echo-jwt não adotado: middleware próprio p/ fail-closed/kid/allowlist).
2. ~~Emissor + middleware~~ — HS256, `kid` exato, allowlist, `WithExpirationRequired`/`WithIssuedAt`/relógio injetável; claims só `sub`/`papel`/`iat`/`exp`; `Exigir` fail-closed (lista vazia = 403), 401 com `WWW-Authenticate: Bearer`, corpos genéricos.
3. ~~Limitador + métricas~~ — Redis INCR + `EXPIRE NX` (falha nova não estende a janela), chave = prefixo + SHA-256 (sem e-mail/IP no Redis); Redis fora → memória com teto de 10k entradas (cheio no fallback = bloqueia; bug de bloquear com Redis saudável pego antes do commit); `auth_login_total{resultado}`, `auth_login_duracao_segundos` (sem WithUnit — o exporter anexaria `_seconds`), `auth_ratelimit_bloqueios_total{escopo}`, `auth_refresh_reuso_total`; `resultado`/`escopo` na allowlist.
4. ~~Testes + depguard~~ — 12 tokens hostis + expirado; matriz 7×4 do middleware; limitador em memória (relógio manual), mapa cheio, Redis real (hash na chave, PTTL real, EXPIRE NX, Limpar) e indisponível (log sem a chave); métricas no registry real. `jwt-boundary` provada com violação proposital (cache → jwt-boundary; catalogo → catalogo-domain), revertida. golangci-lint 0 issues; suíte completa `-race` verde.
5. **Auditoria (2026-09-29, `security`)**: REPROVADA em 1 item — item 10: `prometheus/client_model` promovida a direta (teste usa `dto.MetricFamily`) sem registro no lib.md → **corrigido** (linha no lib.md, OSV sem vulns); reauditoria escopada ao item 10 (§6.4.4): go.mod × lib.md conferido — as 2 deps diretas novas (`golang-jwt/jwt/v5`, `client_model`) registradas → **APROVADA**. Não-bloqueantes registrados no state.md: TOCTOU marginal entre `Bloqueado`/`RegistrarFalha`; sem `nbf`/`aud`/`iss` (emissor único); teto de 10k fixo; DoS por encher o mapa durante queda do Redis = trade-off aceito pelo usuário ("nunca sem limite").

---

### Task 0007 — Stack de observabilidade (E0d 2/2): EM PR #31 (auditoria APROVADA, CI verde; merge aguardando autorização)

### Plano da task 0007

1. ~~PRD 0007~~ — `76af745` (Context7 `/grafana/loki`, `/grafana/alloy`; imagens pinadas + arm64 verificado no lib.md).
2. ~~Compose~~ — `8c55094`: `docker-compose.observability.yml` (só Grafana publica, 127.0.0.1; `mem_limit` por serviço); app no compose dev (profile `app`, `depends_on: service_healthy` — exigência herdada da 0005 cumprida); `rabbitmq_prometheus`; init `pg_monitor`; `.env.observability.example`; `make obs-up/obs-down`.
3. ~~Prometheus/Loki/Alloy~~ — `86986d7`: 8 jobs (incl. `/metrics/detailed` p/ filas); Loki 14d via compactor + limites (sem retenção por tamanho nativa — desvio registrado no PRD); Alloy só projetos `morfeu*`, labels `container`/`service`.
4. ~~Grafana~~ — `738c49e`: datasources `prometheus`/`loki`, 3 dashboards (API golden signals, Infra USE, Mensageria), 6 alertas (lista fechada) → contact point Discord via env.
5. ~~Teste + runbook~~ — `aba6e26`: `test/observabilidade` (Prometheus+promtool, Loki ready, Alloy fmt, Grafana provisioning via API: 3 dashboards/2 datasources/6 regras/contact point/401 anônimo; compose só publica Grafana) — **5/5 verdes**; `docs/observabilidade.md`.
6. ~~Verificação manual (CA05)~~ — stack completa em projeto isolado `morfeu-verif` (override sem portas publicadas: o host já tinha 5432/5672/3000/9090 ocupados por outro projeto do usuário, que não foi tocado; senhas aleatórias descartáveis): **8/8 targets up**; golden signals por rota (`/filmes` 200×3; 404 sem label de path); `criar-filme` → log "mensagem consumida" no Loki com `trace_id`; 3 dashboards; **6 regras avaliando sem erro** (todas inactive). Ajustes descobertos: node-exporter sem `rslave` (WSL), regex do Alloy `morfeu.*`. Stack e volumes de verificação removidos. golangci-lint 0 issues. Diff: 29 arquivos.
7. **Auditoria APROVADA (2026-09-29)** — passe único (`qa`): itens 1–5, 7, 9, 10, 13 conformes (PromQL dos alertas revisado). Não-bloqueantes aplicados: poll com deadline também p/ alert-rules/contact-points e `t.Parallel()` nos 4 testes de container (suíte 23s com imagens em cache). Registrados: desvio da retenção por tamanho do Loki (PRD RF03); cAdvisor monta `/var/run` inteiro (padrão da imagem — revisar no hardening pós-E0c-CD). PR #31.
8. Merge após CI verde (aguardando autorização do usuário).

---

**Task 0006 — Instrumentação da app: OTel + /metrics + logs correlacionados (E0d, parte 1/2): IMPLEMENTAÇÃO COMPLETA e validada localmente (2026-09-29); gate → PR** (branch `feature/0006-instrumentacao-otel-metricas`, criada da main pós-0005 `c35957d`; task `docs/tasks/0006-instrumentacao-otel-metricas.md`). E0d dividida (§6.3): 0006 = app Go, 0007 = stack de observabilidade. E0c-CD bloqueada pela VM Oracle (confirmado pelo usuário 2026-09-29).

### Task 0006 — Instrumentação da app (E0d 1/2): CONCLUÍDA e MERGEADA (histórico)

**Merge `02831dc` (PR #30, 2026-09-29).**

#### Plano executado da task 0006

1. ~~PRD 0006~~ — `8c58a5a` `docs/prd/0006-instrumentacao-otel-metricas.md` + lib.md (OTel core/SDK 1.46.0, exporter Prometheus 0.68.0, client_golang 1.24.1, otelecho **0.70.0** — 0.71 depreca o módulo em favor de `echo-otel/v4` v4.0.0 de 1 dia; débito registrado —, otelpgx 0.12.0; echo 4.15.0→4.15.4 por arrasto). Desvio registrado: "100% erros" exige tail sampling → fica p/ o collector/Tempo (E6/E10).
2. ~~`internal/telemetria`~~ — `db802f8`: TracerProvider `ParentBased(TraceIDRatio 0.1)` + `ExporterDescarte`; MeterProvider → exporter Prometheus em registry dedicado (sem scope/target info); **view com allowlist de atributos** (`http.route`, método, status, `fila`) — cardinalidade garantida no SDK, não só no teste; `MiddlewareHTTP` (otelecho, skip `/metrics` e `/health`); gauges `morfeu_outbox_pendentes`, `morfeu_outbox_lag_segundos`, `morfeu_dlq_mensagens{fila}` no scrape.
3. ~~Logger~~ — `a446262`: core de redação (substring p/ senha/password/token/authorization/secret/cartao/card; `pan` só exato — senão redigiria `span_id`) + `ComTrace`.
4. ~~Broker/outbox~~ — `79441f7`: span `consumir <fila>` filho do traceparent; `ProfundidadeFila` (passiva); `LagSegundos` (query sqlc); log do consumer correlacionado.
5. ~~Wiring~~ — `50cc45f`: `main.go` (telemetria, otelpgx, otelecho primeiro middleware, `/metrics`, request log com trace_id) + integração CA03 (fontes reais PG/RabbitMQ) e CA05 (tracetest).
6. ~~Validação~~ — suíte completa `-race -tags=integration` verde; golangci-lint 0 issues (refatoração de gocognit em `runServer`/`RegistrarMensageria`); govulncheck (Go 1.25.14) **No vulnerabilities**. Diff: 25 arquivos.
7. **Auditoria APROVADA (2026-09-29)** — passe único (`security`): itens 1–5, 7, 9–11, 13 conformes; não-bloqueantes registrados no state.md (redação não desce em `zap.Any`/`Object`; `/metrics` na porta pública até a E0c-CD; migração do otelecho) + guard-rail do teste que troca globais OTel adicionado. PR #30.
8. Gate: CI verde → merge (aguardando autorização do usuário).

---

### Task 0005 — Consumidor idempotente + DLQ (E0b 2/2): CONCLUÍDA e MERGEADA (histórico)

**Merge `c35957d` (PR #29, 2026-09-29).** Auditoria APROVADA; CI verde após bump de segurança (`51e7fc3`: amqp091-go 1.15.0 / x/text 0.39.0 — advisories publicados após a 0002 derrubavam o govulncheck).

**Task 0005 — Consumidor idempotente + DLQ (E0b, parte 2/2): IMPLEMENTAÇÃO COMPLETA e validada localmente (2026-09-29); gate pré-push → PR** (branch `feature/0005-consumidor-idempotente`, criada da main pós-0002 `a1d1924`; task `docs/tasks/0005-consumidor-idempotente.md`). Épico E0 já refinado → PRD 0005 consome o refinamento §"Task E0b" (parte consumidora) + ADR 0007 + contrato de envelope do PRD 0002 (RF05), sem nova rodada de agentes (§6.2.5).

### Plano executado da task 0005

1. ~~PRD 0005~~ — feito, `docs/prd/0005-consumidor-idempotente.md` (skills: `golang-concurrency`, `golang-database`). Decisões de abertura: migrations separadas 003 (plataforma) / 004 (catálogo) p/ isolar blocos sqlc; compose sem mudança (não há serviço de app — `depends_on` vai p/ E0c-CD).
2. ~~Migrations~~ — `6945020`: 003 `processed_messages` (PK `message_id, consumidor`) + 004 `catalogo_filmes_projetados` (coluna `aplicacoes` = prova de idempotência); blocos sqlc separados por ownership.
3. ~~Runtime do consumer~~ — `656c2d4` `internal/broker/consumer.go`: canal próprio, prefetch 1, `ErrPermanente`→nack sem requeue (DLQ), outro erro→requeue (até `x-delivery-limit`), resubscrição após reconexão, handler com `WithoutCancel`+30s no shutdown, `Conectado()`.
4. ~~Dedup + projeção~~ — `30b557b`: `outbox.ProcessarUmaVez`/`NovoHandler` (dedup e efeito na mesma TX; `message_id` inválido→permanente) + `catalogo.ProjetarFilmeCriado`.
5. ~~Wiring + health~~ — `8b5809f`: consumer só em `-mode=worker|all`, mesmo WaitGroup do relay; `/health` ganha `rabbitmq` só com broker (`omitempty` — JSON da E0a intacto). Compose sem mudança (sem serviço de app; `depends_on` → E0c-CD).
6. ~~Testes~~ — `93411cb`: 6 cenários de integração (CA01 dedup 2×→1, CA02 falha antes do commit, CA03 malformado+sem message_id→DLQ com fila viva, CA04 delivery-limit, CA06 shutdown com entrega em curso, CA05 walking skeleton CLI→projeção) + health com/sem broker. Evidência: suíte completa `-race -tags=integration` verde via container; golangci-lint 2.12.2 **0 issues**; `sqlc generate` sem diff + `sqlc vet` limpo. Diff: 25 arquivos (≤30).
7. **Auditoria APROVADA (2026-09-29)** — passe único de julgamento (`qa`), itens 1–5, 7, 9, 10, 13 conformes; não-bloqueantes: (a) e2e na fila real depende de ausência de `t.Parallel()`/`-shuffle` → guard-rail em comentário adicionado; (b) `novoFilmID` aleatório — colisão teórica aceita; (c) pirâmide achatada para integração é proposital (dedup é interação transacional real).
8. Gate: gitleaks pré-push → push → PR → CI verde → passe único de julgamento → merge (fecha o walking skeleton assíncrono do E0b).

---


### Task 0002 — Outbox + RabbitMQ: lado produtor (E0b 1/2): CONCLUÍDA e MERGEADA (histórico)

**Merge `a1d1924` (PR #19, 2026-07-13).** Auditoria APROVADA 2026-07-13 (ver Auditorias). Entregue: `internal/broker` (fronteira única do amqp091-go: confirms síncronos 5s, reconexão NotifyClose+backoff 1s..30s com jitter, `DeclararTopologia` idempotente — topic + DLX + quorum queue `x-delivery-limit=3` + DLQ), `internal/outbox` (WithTx/Enqueue/Pendentes + relay 750ms com SKIP LOCKED, `published_at` só pós-confirm, traceparent W3C via otel/propagation sem SDK), `CreateFilm` na mesma TX + CLI `criar-filme` + `-mode api|worker|all` com shutdown ordenado, compose RabbitMQ (watermarks 768MB/512MB, management só localhost, credenciais via .env), depguard `broker-boundary` (CA08 provado com violação proposital), migration 002, suíte integração testcontainers PG+RabbitMQ com goleak (CA01–CA05/CA07–CA09; CA06 nos gates do CI). Correções descobertas na validação: CA02 semântico (JSONB normaliza round-trip), CA03 porta fixa de host (stop/start remapeia efêmera; `moby/moby/api` test-only no lib.md), smoke do CI em `-mode=api` (`ce7f821` — modo all exige broker no boot), misspell ignore-rules pt-BR, `startRelay` extraído (gocognit).

**PRD `docs/prd/0002-outbox-rabbitmq.md`** (2026-07-12, just-in-time, §6.2.5) — consumido.

**Divisão da E0b (decisão de abertura do PRD 0002, roles.md §6.3):** ~32–33 arquivos reais estimados (> 30) → **0002 = produtor** (mergeada) + **0005 = consumidor**. Exigências do refinamento seguem válidas, distribuídas; o walking skeleton assíncrono fecha na 0005. Shell SPA segue **0006 candidata**.

---

### Task 0004 — Pipeline de CI + build ARM64 (E0c-CI): CONCLUÍDA e MERGEADA (histórico)

**Merge `7585522` (PR #6, 2026-07-12).** Pipeline real de gates em runner ARM64 nativo substituiu o placeholder; débito de lint da E0a quitado (0 issues); wait strategies no lugar de `time.Sleep`; supply chain pinada (actions por SHA verificados, gitleaks por checksum, base image por digest, Dependabot); `.gitleaks.toml`; Dockerfile corrigido (copia `migrations/`); governança no modo-alvo (§6.4 — a partir deste merge, os itens mecânicos da auditoria rodam no CI e a skill `auditoria` reduz ao passe único de julgamento). Validações: CA01 run real verde; **CA02/CA03: 6/6 PRs de prova bloquearam no job esperado** (#7 govulncheck, #8 errcheck, #9 data race, #10 SQL inválido, #11 secret, #12 migration sem down + prova positiva do path-filter; evidência com links comentada no PR #6; provas fechadas sem merge); **CA08: branch protection na main** (check `ci` + `enforce_admins`); 3 fixes pós-push (Go `1.25.x`+`check-latest` p/ govulncheck, `GOTOOLCHAIN=auto` p/ sqlc, chave revive inválida removida) cobertos por passe de julgamento delta APROVADO (ver Auditorias).

Decisão de abertura registrada: shell SPA ficou **fora** (task própria, 0005 candidata) — desvio consciente da recomendação não-bloqueante do refinamento.

#### Plano executado da task 0004

1. ~~PRD 0004 (`criar-prd`)~~ — feito, `docs/prd/0004-pipeline-ci-arm64.md` (runner ARM64 c/ fallback documentado; actions registradas no PRD, não no lib.md; lista dos 43 issues por linter/arquivo).
2. ~~Quitação do débito de lint~~ — feita em 6 commits por linter (`790b190` errcheck, `ea6f7ac` revive, `0c8d6d5` staticcheck, `3e90383` gosec, `d2c00f7` errorlint, `c9acf40` funlen): `golangci-lint run ./...` → **0 issues**; asserts intactos; suíte completa `-race` verde.
3. ~~Wait strategies~~ — feito (`384db35`): PG `wait.ForLog(...).WithOccurrence(2)` + timeout 120s (o log do initdb aparece 2×; a 2ª ocorrência elimina a corrida que o sleep mascarava), Redis timeout 90s; 12 `time.Sleep(1s)` removidos (mantido o de 100ms do `TestCacheTTLRespected` — é parte do assert de PTTL); gofmt pendente aplicado (`//go:build`, imports).
4. ~~Workflow real~~ — feito (`50d66b7`): `ci.yml` com jobs `gates` (lint 2.12.2 → vet → gitleaks 8.30.1 c/ checksum → govulncheck 1.6.0 → sqlc 1.31.1 vet+diff), `test` (suíte `-race` completa, timeout 20 min), `changes`+`migrations` (condicional por path, PG efêmero, migrate 4.19.1 up→down→up), `build` (buildx linux/arm64 nativo → smoke `/health` 200 → push GHCR sha+latest só na main), `ci` (check consolidador p/ branch protection). Actions pinadas por SHA; `permissions` mínimas; Dependabot (3 ecosystems); Dockerfile por digest. **`.gitleaks.toml`** novo: allowlist do falso positivo conhecido (skill api-design). **Correção descoberta:** Dockerfile não copiava `migrations/` — a imagem scratch subia e morria (main.go roda `migrate.New("file://migrations")` no startup); validado com smoke local (PG+Redis reais: `/health` 200, `/filmes` 200).
5. ~~Governança~~ — feita (`5396bd9`): roles.md §6.4 no modo-alvo (§6.4.5 agora lista mecânicos sem job próprio: cobertura, ≤30 arquivos, go.mod×lib.md, plan/state — ficam no julgamento); skill `auditoria` modo-alvo; CLAUDE.md invariante 2 sem cláusula de transição.
6. ~~Gate: auditoria pré-push (última no modo transição) → push → PR → validar CA01 no run real~~ — feito: auditoria APROVADA (2026-07-12), PR #6 aberto, CI verde após 3 fixes (`ad4710d` chave revive inválida no schema v2; `9ae2860` Go `1.25.x`+`check-latest` p/ o gate do govulncheck contar patches de stdlib; `f9a1ba4` `GOTOOLCHAIN=auto` no install do sqlc 1.31.x, que exige Go ≥ 1.26).
7. ~~PRs de prova descartáveis (um por gate) com evidência registrada no PR final (CA02/CA03)~~ — feito: **6/6 bloquearam** — #7 govulncheck (job `gates`), #8 errcheck (`gates`), #9 data race (`test`), #10 SQL inválido (`gates`), #11 secret dummy (`gates`; AWS keys são pré-bloqueadas pelo push protection do GitHub — padrão genérico de alta entropia usado), #12 migration sem `down` (`migrations` disparou pelo path-filter **e** falhou — prova positiva do condicional; no PR #6 o job foi `skipped` — prova negativa). Evidência consolidada em comentário no PR #6; provas fechadas e branches deletadas.
8. ~~Branch protection exigindo check `ci` via `gh api` (CA08)~~ — feito: `required_status_checks.checks=[ci]`, `enforce_admins=true`.
9. Merge do PR #6 (após CI verde do commit de docs) → registrar merge no próximo ciclo de docs; a partir do merge, a skill `auditoria` opera no modo-alvo (passe único de julgamento).

---

**Contexto anterior (2026-07-11):** Task 0003 **CONCLUÍDA e mergeada** (PR #4, merge `a391cdb`, 2026-07-10 — auditoria APROVADA, ver seção Auditorias). **Respostas do refinamento E0 registradas e mergeadas** (PR #5, `ff06e5c`, 2026-07-11): usuário aprovou a reordenação do épico (conformidade ✅ → E0c-CI → E0b → E0c-CD → E0d; roadmap atualizado) e autorizou o **ADR 0007 "Mensageria: RabbitMQ"** (criado com parecer do `arquiteto`).

### Task 0003 — Conformidade package-by-domain (pré-E0b): CONCLUÍDA (histórico)

Branch `refactor/0003-conformidade-package-by-domain` (removida pós-merge); task `docs/tasks/0003-conformidade-package-by-domain.md`; PRD `docs/prd/0003-conformidade-package-by-domain.md`. Objetivo: realinhar a E0a ao layout do ADR 0003 (catálogo → `internal/catalogo/`, health → `internal/health/`, `cmd/app` → `cmd/morfeu`, `depguard`, `sqlc.yaml`/Makefile/Dockerfile) + teste HTTP real de `GET /filmes` (exigência da auditoria de 2026-07-10). Critério central: suíte da E0a passa sem alteração de asserts — **confirmado** (ver diffs dos commits de movimentação: só imports/paths mudaram).

### Implementação concluída (2026-07-10) — 14 commits no branch (2 planejamento + 10 implementação + 2 docs), 26 arquivos no diff

1. `855898f` — `cmd/app` → `cmd/morfeu`.
2. `95f72a8` — `internal/handler/health*` → `internal/health/` (pacote de plataforma); `main.go` ajustado.
3. `b378bc6` — `internal/handler/film*` + `internal/service/*` → `internal/catalogo/` (`handler.go`/`handler_test.go`/`service.go`/`service_test.go`, package `catalogo`).
4. `973a046` — `queries.sql` → `internal/catalogo/queries.sql`; `sqlc.yaml` regenerado em `internal/catalogo/db/`; `internal/db` antigo removido.
5. `2abc310` — imports de `internal/integration_test.go` e `internal/integration_cache_test.go` ajustados (**zero assert alterado** — diff confirmado só em linhas de import/`NewFilmService`).
6. `d1cc7c6` — Makefile/Dockerfile apontam para `./cmd/morfeu`.
7. `9f62ba4` — `FilmService` passa a depender de `db.Queries` (gerado) em vez do `pgxpool.Pool` cru (pré-requisito do depguard de fronteira — wiring puro, zero mudança de comportamento).
8. `44ec0e2` + `80284cc` — `.golangci.yml` migrado para config v2 (v1 nunca validou — golangci-lint 2.x rejeitava o arquivo) com regra `catalogo-domain` (list-mode `strict`): cobre as duas fronteiras do ADR 0003 (módulo↛módulo e domínio↛driver de infra) num único allowlist; testes (`_test.go`) excluídos (ADR 0006 exige testcontainers reais no teste). Testado com violação proposital (import de `pgx` e de `internal/health` em `handler.go`) — bloqueado nos dois casos, revertido antes do commit.
9. `d687f32` — novo teste `TestListFilmsHTTP_Integration` em `internal/catalogo/handler_test.go` (padrão `health_test.go`: testcontainers PG+Redis reais, `echo.Context` real via `httptest`, chama `handler.ListFilms` — não o service): 200, `Content-Type: application/json`, corpo decodifica em `[]FilmResponse`. Comentário enganoso removido.

**Desvios do PRD** (documentados nos commits, exigidos pelos próprios critérios de aceite — nenhum é feature nova nem mudança de comportamento observável):
- `sqlc.yaml` estava em formato `version: "1"` inválido para a estrutura usada — nunca rodou de verdade (código gerado anterior era escrito à mão). Corrigido para `version: "2"` (`sql: []`) + campos `emit_*` atuais.
- `.golangci.yml` idem — v1 não roda no golangci-lint 2.12.2 (container padrão). Migrado para v2.
- Dockerfile tinha `golang:1.21-alpine`, incompatível com `go.mod go 1.25.0` (`docker build` falhava antes desta task) — corrigido para `golang:1.25-alpine`, necessário para CA06.
- `FilmService.NewFilmService` mudou de assinatura (`*pgxpool.Pool` → `*db.Queries`) — necessário para o depguard não banir a própria construção do serviço; chamadas em `main.go` e nos testes de integração ajustadas (`db.New(pool)`), sem alterar nenhum assert.

Nenhum destes é overengineering nem feature nova — todos são pré-condições descobertas durante a implementação para que os próprios critérios de aceite do PRD (CA03, CA04, CA06) fossem alcançáveis (o padrão já visto no fix emergencial de `fix/e0a-build-quebrado`: configs e Dockerfile nunca tinham sido executados de verdade).

**Evidência de verificação:** `go build ./...`, `go vet ./...` limpos; suíte completa (`go test -race -tags=integration ./...`) verde via container `golang:1.25` (unit + integração com testcontainers reais); `sqlc generate` sem diff (RNF03/CA04); `golangci-lint run --enable-only=depguard` limpo + violação proposital bloqueada (CA03); `docker build` funcional com `cmd/morfeu` (CA06).

**Pendências:** ~~PR~~ → **PR #4 aberto, CI verde e mergeado em 2026-07-10** (merge `a391cdb`; branches local/remota removidas).

Contexto anterior: **task 0001 (E0a) concluída e mergeada** (merge `6916ab6`); **fix emergencial do build mergeado** (PR #3, `fa2a136`, 2026-07-10 — auditoria APROVADA abaixo). **Épico E0 refinado (2026-07-09)** — cerimônia por épico (roles.md §6.14) registrada em [`docs/refinamentos/E0-walking-skeleton.md`](docs/refinamentos/E0-walking-skeleton.md): 5 pareceres, 5 divergências resolvidas e **2 perguntas escaladas ao usuário**.

### Perguntas escaladas ao usuário (§6.14.7) — RESPONDIDAS em 2026-07-11

1. **Aval para reordenar o épico** (E0c → E0c-CI + E0c-CD, sequência conformidade → E0c-CI → E0b → E0c-CD → E0d) → **✅ APROVADO**; roadmap e refinamento atualizados.
2. **Autorização para criar o ADR "Mensageria: RabbitMQ"** → **✅ AUTORIZADO**; ADR 0007 criado (`docs/adr/0007-mensageria-rabbitmq.md`), desbloqueando o PRD da 0002 (E0b).
3. (Ação do usuário, não bloqueia PRDs) **Item 0**: iniciar criação da conta Oracle PAYG + VM A1 — única dependência externa do M1. **Segue em aberto.**

### Fix emergencial (2026-07-10, branch `fix/e0a-build-quebrado`, commit `ab70d13`)

Ao preparar a task de conformidade, descobriu-se que **a E0a mergeada nunca compilou**: `go.sum` ausente do repo, erros de compilação em produção (`models.go`, `main.go`) e em TODOS os arquivos de teste, testcontainers 0.31.0 incompatível com o grafo de deps, cache gravado **sem TTL** (violando CA05 do PRD 0001) e testes de handler com mock camada a camada (vedado pelo ADR 0006) quebrados por NPE. Nada disso foi detectado porque não havia toolchain Go no ambiente e o CI é placeholder — evidência concreta da prioridade do E0c-CI. Corrigido: suíte completa verde com `-race` (unit + integração com testcontainers reais). Toolchain Go 1.24.5 instalado user-level em `~/.local/go`; `-race` roda via container `golang:1.25` (sem gcc no WSL); lib.md atualizado (Go 1.25 implementada; testcontainers v0.43.0).

### Plano da task 0003 (status: implementação concluída, ver seção acima)

1. ~~PRD 0003 (`criar-prd`)~~ — feito, `docs/prd/0003-conformidade-package-by-domain.md`.
2. ~~Movimentação package-by-domain em blocos verificáveis~~ — feito (commits 1–4 acima).
3. ~~`sqlc.yaml` (paths por módulo) + regeneração sem diff manual; Makefile~~ — feito (commits 4, 6).
4. ~~`depguard` no `.golangci.yml`~~ — feito (commits 8).
5. ~~Teste de integração HTTP real de `GET /filmes`~~ — feito (commit 9).
6. ~~Gate~~ — feito: critério central confirmado (asserts intactos); auditoria APROVADA (2026-07-10); PR #4 mergeado (`a391cdb`, 2026-07-10).

## Auditorias

- **2026-07-13 — APROVADA** (task 0002 — outbox + RabbitMQ lado produtor, PR #19, 14 commits, 36 arquivos): modelo híbrido §6.4 modo-alvo. Mecânicos: CI do PR verde 6/6 (gates/test/migrations/changes/build/ci) após 1 fix (`ce7f821` — smoke em `-mode=api`: o modo `all` passou a exigir RabbitMQ no startup e o broker não é infra do smoke; validado localmente antes do push); gitleaks v8.30.1 histórico completo limpo (2×, incluindo pós-fix). Julgamento em passe único (`qa`, Sonnet) ✅ em 1–5, 7, 9, 10/11, 13: **36 > 30 arquivos julgado não-bloqueante** (13 não-autorais: 6 docs de controle + 5 gerados sqlc + go.mod/go.sum; 23 autorais ≤ estimativa ~27 da divisão E0b registrada na abertura do PRD; mesma régua do precedente 2026-07-07); desvios de teste preservam a intenção dos CAs (CA02 semântico — JSONB normaliza round-trip; CA03 porta fixa — stop/start remapeia porta efêmera, dep test-only `moby/moby/api` promovida com OSV limpo); fail-fast do relay no boot coerente com RF04/RNF06 (resiliência exigida é para queda em runtime — coberta por `TestRelay_BrokerIndisponivelNaoCrashaEReentrega` com stop/start real — e o idioma de boot já existia p/ DB/migrations); logs sem payload/credencial (RNF01); suíte idempotente (container por pacote efêmero, aggregate_id único, sem t.Parallel). **Achados não-bloqueantes:** (1) `TestOutbox_PendentesContagem` conta globalmente — quebra silenciosa se `t.Parallel()` for introduzido no arquivo (registrar guard-rail/comentário em task futura); (2) credenciais de dev local previsíveis, aceitável (mesmo padrão do postgres/postgres); (3) 2ª vez que o teto de 30 é ultrapassado por composição — sugerido ao usuário avaliar se §6.3 deveria contar gerados/controle à parte (governança, fora deste gate). **Merge liberado.**

- **2026-07-12 — APROVADA (passe de julgamento delta, pré-merge do PR #6)** (task 0004, delta pós-auditoria: commits `ad4710d`+`9ae2860`+`f9a1ba4`, 2 arquivos +19/−6 — escopo restrito ao delta conforme §6.4.4): CI do PR #6 verde (gates/test/changes/migrations-skipped/build/ci); revisor único (`security`, Sonnet/medium) ✅ em 1, 3, 4, 5 (2/7/9 N/A — sem decisão arquitetural, testes ou logging no delta; 10/11 confirmado `go.mod`/`go.sum` sem diff); supply chain: nenhum SHA de action tocado; `go-version: 1.25.x`+`check-latest` julgado consistente com a filosofia de pinagem ("pinar o mutável não-confiável" — o toolchain vem da action oficial pinada com validação de integridade; pinar 1.25.0 exato deixaria o gate do govulncheck vermelho a cada CVE de stdlib) — risco residual de reprodutibilidade classificado informativo/baixo; `GOTOOLCHAIN=auto` escopado ao `go install` do sqlc, download verificado via GOSUMDB — risco baixo. Nenhuma correção obrigatória. **Merge liberado.**
- **2026-07-12 — APROVADA** (task 0004 — pipeline de CI + build ARM64, branch `chore/0004-pipeline-ci-arm64`, 12 commits, 25 arquivos — **última auditoria pré-push do modo transição**): mecânicos na sessão principal — suíte completa `-race` verde 2× (testcontainers reais, container `golang:1.25`); `golangci-lint run ./...` **0 issues** (v2.12.2 — débito de 43 quitado); gitleaks 8.30.1 árvore + histórico limpos com o novo `.gitleaks.toml` (2 achados do `dir` = `.env`/`.claude/settings.local.json`, gitignorados, nunca entram no checkout do CI); govulncheck v1.6.0 — 0 exploráveis; sqlc vet + diff OK; go.mod/go.sum sem diff (item 10 N/A); 25 ≤ 30 arquivos; sem schema (item 12 N/A); docker build + **smoke local real** (`/health` 200 com PG+Redis, `/filmes` 200). Julgamento em passe único (`security`, Sonnet/medium) ✅ em todos (1–5, 7, 9, 13 + supply chain): correções de lint mecânicas com asserts intactos confirmadas por diff (CA06); wait `WithOccurrence(2)` corrige corrida real do log duplo do initdb; ADR 0006 preservado (testcontainers reais); **7 SHAs de actions verificados via GitHub API** (= tag GPG do release comentado); allowlist do gitleaks estreita (path único, `useDefault=true`); `permissions` mínimas, sem PAT, checksum do gitleaks verificado antes de executar; Dockerfile por digest e cópia de `migrations/` = DDL público; governança descreve honestamente a cobertura do CI (§6.4.5 lista o que falta). Não-bloqueantes: registrar no PR a verificação dos SHAs; hardening opcional de `contents: read` por job. Push liberado.
- **2026-07-11 — APROVADA** (registro das respostas do refinamento E0 + ADR 0007, branch `chore/respostas-refinamento-e0`, 8 arquivos, 100% documentação): mecânicos na sessão principal — gitleaks árvore+histórico (1 achado = falso positivo conhecido do template), 8 ≤ 30 arquivos, itens 6/10/11/12 N/A (sem código/deps/schema). Julgamento em passe único (`qa`, Sonnet/medium) ✅: escopo exato (2 respostas do usuário + pós-merge da 0003, nenhum arquivo alheio); ADR 0007 sem conflito com ADRs 0001–0006 — referências cruzadas verificadas linha a linha contra o conteúdo real — e fiel às exigências da task E0b do refinamento (topologia, confirms, dedup, watermarks, envelope); divergência da descoberta registrada honestamente (fila em PG mais simples; rota de reversão concreta); sem secrets/PII (ocorrências de "guest/secret" são instrução normativa). **Correção obrigatória aplicada no ato:** data do merge do PR #4 corrigida de 2026-07-11 para **2026-07-10** em state.md/plan.md (merge real `a391cdb` em 2026-07-10 18:09 GMT-3, verificado via `git show`/`gh pr view`; hash e nº do PR estavam corretos). Push liberado.
- **2026-07-10 — APROVADA** (task 0003 — conformidade package-by-domain, branch `refactor/0003-conformidade-package-by-domain`, 14 commits, 26 arquivos): modelo híbrido §6.4, transição pré-push completa. **Mecânicos na sessão principal:** suíte completa verde com `-race` via container `golang:1.25` (unit + integração com testcontainers reais; `internal` 46s, `catalogo` 6.5s, `health` 11.8s); gitleaks árvore+histórico — 1 achado = falso positivo conhecido (`sk_live_abc123`, exemplo didático em `.claude/skills/api-design/SKILL.md`, herdado do template); govulncheck — 0 vulnerabilidades exploráveis (1 em pacote importado e 19 em módulos requeridos, nenhuma alcançável); go.mod/go.sum sem diff (item 10 N/A); 26 ≤ 30 arquivos; sem mudança de schema (item 12 N/A); golangci-lint v2: **depguard limpo** (CA03) — 43 issues de outros linters (errcheck 26, revive 6, staticcheck 4, errorlint 4, gosec 2, funlen 1) são débito pré-existente da E0a surfaced pela migração do config v2 (o v1 nunca rodou) → registrado como pendência p/ o gate de lint do E0c-CI. **Julgamento em passe único (`qa`, Sonnet/medium) ✅ em todos os itens:** (1) CA01–CA06 verificados com evidência (asserts intactos nos testes movidos, confirmado por diff; teste HTTP real chama `h.ListFilms(c)` via `httptest`); 4 desvios da nota de implementação confirmados como pré-condições reais, zero mudança de comportamento; (2) layout e depguard materializam as duas fronteiras do ADR 0003, exclusão `!$test` compatível com ADR 0006; (3) sem abstração nova; (4/5) 26 arquivos todos previstos no PRD/nota; (7) containers efêmeros + `defer Terminate`, dados semeados por teste, sem dependência de ordem; (9) logs sem PII/credencial; (13) plan/state fiéis (resumo "8 commits, 12 arquivos" corrigido no ato). **Achados não-bloqueantes:** `time.Sleep(1s)` como espera de prontidão em 3 arquivos de teste (padrão herdado da E0a) → substituir por estratégias `wait.For*` em task futura de hardening da suíte. Push liberado.
- **2026-07-10 — APROVADA** (fix emergencial do build da E0a, branch `fix/e0a-build-quebrado`, 4 commits, 16 arquivos): modelo híbrido §6.4 — mecânicos na sessão principal (gitleaks: 1 falso positivo em doc; govulncheck: 0 exploráveis; 16 ≤ 30 arquivos; go vet ok; suíte completa verde com `-race`, testcontainers reais), julgamento em passe único (`qa`, Sonnet/medium) ✅ em todos os itens: (1) TTL 0→5min ATENDE CA05/RN03 do PRD 0001, validado por `TestCacheTTLRespected` com PTTL; (2) ADR 0004 ok (pgx v5 `ConnConfig` aninhado correto) e ADR 0006 ok (remoção dos mocks camada-a-camada vedados); (3) sem abstração nova; (4/5) 16 arquivos = fix de build + registro do refinamento E0 + controle; (7) containers efêmeros com `defer Terminate` + `FlushDB` por cenário; (9) logs sem URL/credencial/payload; (13) plan/state/lib fiéis ao diff. **Achado não-bloqueante** (pré-existente, fora do escopo do fix): `TestListFilmsE2E_FullStack` chama `svc.ListFilms` direto — o handler HTTP real de `GET /filmes` (RF03/CA04: status 200, `Content-Type: application/json`) não tem teste de integração no padrão de `health_test.go`, e o comentário em `internal/handler/film_test.go:9-10` afirma cobertura que não existe → **registrado como exigência da task de conformidade pré-E0b**. Push liberado.
- **2026-07-08 — APROVADA** (governança de economia de tokens, branch `chore/governanca-economia-tokens`, 4 commits): primeiro passe no **modelo híbrido novo** (§6.4 — revisor único de julgamento, `security` em Sonnet/medium). Mecânicos na sessão principal: 29 ≤ 30 arquivos, árvore limpa, grep de secrets no diff limpo (gitleaks ausente — instalar segue pendente), sem código Go/deps/migrations (itens 6, 10–12 N/A). Julgamento ✅ em todos: escopo = exatamente os 7 pontos autorizados pelo usuário (§8); sem conflito com ADRs 0001–0006; sem overengineering (mudança REDUZ cerimônia); hook alterado só com `--model haiku` (`--dangerously-skip-permissions` preexistente, pendência conhecida do state.md, sem regressão); settings sem secrets; consistência roles.md × CLAUDE.md × skills × agentes íntegra; numeração §4.4/§6.4.1–6/§6.14.1–8/§6.15.1–8 sem gaps. Sem correções obrigatórias. Push liberado.
- **2026-07-08 — Correção de Bloqueadores (reauditoria pendente)** (commits `524a292`+`3e6f727`+`a244f98`+`2957a13`+`6f2fefc`+`007882e`): Resolvidos 9 bloqueadores pré-push:
  - **Security** (3 itens): (7) docker-compose sem credenciais versionadas → .env.docker-compose; (8) golang-jwt v5.2.2+ atualizado em go.mod; (9) lib.md com zap, uuid, testcontainers registrados.
  - **QA** (6 itens): (1) cache hit/miss com Redis real (testcontainers) → `internal/integration_cache_test.go`; (2) migrations up/down/up idempotente → `TestMigrationsUpDownUpIdempotent`; (3) graceful degradation Redis → `TestGracefulDegradationRedisUnavailable`; (4) health endpoint tests → `internal/handler/health_test.go`; (5) handler→service e2e → `TestListFilmsE2E_FullStack`; (6) containers efêmeros + ON CONFLICT removido de migrations.
  - Todos os testes usam testcontainers reais (não mocks); containers por suite com `defer terminate()` para limpeza; 14 commits no total (8 iniciais + 6 correção).

- **2026-07-07 — APROVADA** (docs/lib + vendorização de skills Go, commits `019b664`+`46b7e61`+fix): 13 itens; diff de 49 arquivos justificado (§6.3 rege tasks de implementação; 39/49 são conteúdo de terceiros verbatim — superfície autoral ~13 arquivos; precedente do template com 28 skills). Security ✅ (item 8: zero secrets/PII em greps independentes, docs/lib com placeholders corretos; item 9: N/A + skills de terceiros sem conteúdo malicioso/telemetria/hooks embutidos, allowed-tools escopado; item 11: N/A — markdown puro, lib.md consistente arquivo a arquivo; extra: 4 achados pré-existentes do AgentShield no hook Obsidian avaliados como não bloqueantes → pendência registrada no state.md). QA: 1ª rodada REPROVADO por referência morta a `jpa-patterns` no README.md:58 → corrigida (lista duplicada substituída por referência a roles.md §4.2) + re-grep limpo → **APROVADO** (itens 6/7 N/A-justificados; consistência §4.2/lib.md/docs-lib/§6.9.4/§7/state.md verificada). security-scan (AgentShield) pós-vendorização: nenhum achado nas skills novas.
- **2026-07-07 — APROVADA** (registro da identidade visual, commit `93104b5`): 13 itens verificados; diff de 6 arquivos, 100% documentação (`docs/design/` novo + ponteiros em roles.md/doc.md/state.md). Security ✅ (item 8: zero secrets — 4 blobs base64 validados como woff2 genuínos por magic bytes, zero alta-entropia residual, PII só placeholders; item 9: N/A-justificado + JS do protótipo verificado sem rede/console/storage; item 11: nenhuma dependência nova — fontes embutidas são asset estático; pendência `@fontsource/*`→lib.md formalizada p/ quando a SPA nascer). QA ✅ (itens 6/7 N/A-justificados — sem código de produção no repo; consistência das 5 referências cruzadas confirmada; observação não bloqueante: notação "E0c" vs. E0 item (c) do roadmap). Itens 1–5, 10, 12, 13 na sessão principal: sem PRD/task por ser artefato de governança-design pré-task (mesmo precedente do bootstrap), escopo coerente, 6 ≤ 30 arquivos, lib.md intacto, sem schema, state.md/plan.md atualizados.
- **2026-07-07 — APROVADA** (bootstrap de governança, pré-push inicial): 13 itens verificados; security ✅ (árvore publicável + histórico sem secrets; CVEs do lib.md revalidadas no OSV), qa ✅ (itens 6/7 N/A-justificados — diff 100% documentação; consistência ADR 0006 ↔ doc.md ↔ roadmap confirmada; divergência lib.md "3–5→2–4 jornadas" corrigida no ato).
