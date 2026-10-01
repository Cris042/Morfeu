# Roadmap — Morfeu

> Gerado pela descoberta (`iniciar-projeto`, roles.md §6.15) em 2026-07-06, a partir do parecer de viabilidade do backend-dev e das decisões do confronto multiagente (registradas em `doc.md` §10). Cada épico vira uma ou mais tasks ≤ 30 arquivos via `criar-task` (fluxo completo em `roles.md` §2).

## Visão

Venda de ingressos de cinema online (um cinema; cartaz → sessão → mapa de assentos → checkout Stripe sandbox → ingresso por e-mail) com backoffice mínimo do operador. Projeto de portfólio/aprendizado: Go + Echo, sqlc + pgx, PostgreSQL, Redis (cache), RabbitMQ (saga do checkout), React + Vite, VM Oracle Always Free com observabilidade self-hosted e SLOs validados por teste de carga k6. Detalhes: `doc.md`.

**Calibração declarada:** dev solo · 10–12h/semana · **horizonte honesto: 7–9 meses até o MVP** (cenário de 6 meses exigiria ~15h/sem constantes) · sem prazo fixo — qualidade > data.

**Metodologia:** descoberta → roadmap → task → refinamento (5 agentes) → PRD → implementação → auditoria → push → PR → CI/CD. Princípios do sequenciamento: deploy público na semana 3–4 (de-risker máximo: VM Oracle, pipeline ARM e CI/CD só se provam em produção); fatias verticais; blocos de trabalho por linguagem (Go × React) para reduzir troca de contexto de dev solo.

## Marcos

| Marco | Prova | Épicos |
|---|---|---|
| **M1 — URL pública viva** | walking skeleton deployado com TLS, CI/CD ARM e observabilidade base | E0 |
| **M2 — Operador importa filme real** | TMDB → catálogo no backoffice | E1–E2 |
| **M3 — Dois navegadores disputam o mesmo assento** | a demo mais forte do portfólio (mapa interativo + trava no PG) | E3–E5 |
| **M4 — Compra completa ponta a ponta** ✅ (2026-09-30) | cartaz → assento → Stripe → ingresso no e-mail (saga + compensações) — provado pelo E2E `web/e2e/m4.spec.ts` (gateway e e-mail fakes, CSP de produção, axe); Stripe real em modo teste validado no deploy (E0c-CD) | E6–E8 |
| **M5 — SLO validado sob carga** | p95 < 300 ms @ 300 req/s leitura, erro < 1%, invariante de assento pós-carga | E9–E12 |

## Itens

| # | Épico | Prioridade | Status | Riscos/Dependências | Tasks |
|---|------|-----------|--------|---------------------|-------|
| 0 | **Pré-bootstrap (ambiente)**: mover repo p/ WSL ext4 + IDE modo WSL; criar conta Oracle **PAYG** e provisionar VM A1 (capacidade é loteria — semana 1); criar repo GitHub **público** com secret scanning + push protection; remover `pom.xml` placeholder | alta | pendente | decisões do confronto; sem código envolvido | — |
| 1 | **E0 — Walking skeleton** (reordenado em 2026-07-11, aval do usuário pós-refinamento — `docs/refinamentos/E0-walking-skeleton.md`): (a) ✅ app Go skeleton + compose core (PG/Redis) + migrations golang-migrate + `GET /filmes` seedado c/ cache read-through; (conf) ✅ conformidade package-by-domain (ADR 0003, pré-E0b); (c-CI) pipeline de CI (lint→vet→test -race→govulncheck→gitleaks→build ARM64→GHCR) + shell da SPA — peça do gate híbrido §6.4, sem dependência externa; (b) outbox + RabbitMQ + `-mode=worker` consumidor idempotente (ADR 0007); (c-CD) deploy SSH + Caddy/TLS + smoke na URL pública (bloqueada pela VM Oracle; fura a fila se sair antes); (d) observabilidade base (OTel métricas + Prometheus + Grafana provisionado + logs JSON) | alta | em andamento | item 0; capacidade A1; **M1** | 0001 (a, ✅ PR #1/#3) · 0003 (conf, ✅ PR #4) · 0004 (c-CI, ✅ PR #6) · 0002+0005 (b, ✅ PR #19/#29) · 0006+0007 (d, em andamento) |
| 2 | **E1 — Identidade** (2 tasks): registro/login Argon2id + JWT + RBAC + seed operador; refresh rotativo c/ detecção de reuso + deleção de conta (pseudonimização) | alta | ✅ concluído (2026-09-29) | E0; desenho security inegociável (doc.md §14.4) | 0008 (PR #33) · 0009 (PR #34) · 0010 (PR #35) |
| 3 | **E2 — Catálogo + TMDB** (1–2 tasks): CRUD filmes + import TMDB no backoffice; consolida o padrão sqlc+handler+teste; **M2** | alta | ✅ concluído (2026-09-29) | E0–E1 | 0011 (PR #36) · 0012 (PR #37) |
| 4 | **E3 — Sessões e salas** (2 tasks): domínio + regra de não-conflito (**limpeza de 20 min** — refinamento E3); backoffice **só API** (telas do operador movidas p/ E5/E9 com aval do usuário, 2026-09-29) | alta | ✅ concluído | E2 | 0013 (escrita, ✅ PR #38) · 0014 (leitura, ✅ PR #39) |
| 5 | **E4 — Reserva (backend)** (2 tasks): hold com partial unique index + `expires_at` (TTL 10 min + 1 extensão) + sweeper; endpoint do mapa + cache Redis; teste de corrida canônico (goroutines + barreira + invariante). Refinado em 2026-09-29 (ADR 0008; token de carrinho em cookie; 6 holds/dono; ocupação em endpoint separado com cache 3 s) | alta | ✅ concluído | E3; fluxo crítico 2 | 0015 (trava, ✅ PR #41) · 0016 (ocupação, ✅ PR #42) |
| 6 | **E5 — SPA cliente, parte 1** (**4 tasks** — refinamento E5, ADR 0009): fundação (shell + tokens + cliente `/api` + CI do front) → cartaz + sessões → **mapa de assentos isolado** (dados fake) → integração com a trava (polling 3–5 s) + **E2E do M3 no CI**; telas do operador movidas p/ o E9 (aval do usuário, 2026-09-29) | alta | ✅ concluído (**M3**) | E4; bloco React | 0017 (fundação, ✅ PR #45) · 0018 (cartaz, ✅ PR #46) · 0019 (mapa, ✅ PR #47) · 0020 (integração, ✅ PR #48) · 0021 (E2E do M3, ✅ PR #49) |
| 7 | **E6 — Saga do checkout** (**6 tasks** — refinamento E6, ADR 0010): reserva (holds vendidos ocupam o assento + portas transacionais) → pedido (máquina de estados + criação, gateway fake) → Stripe + webhook + pivô → estorno + reconciliação → observabilidade da saga + consumidor stub + integração ponta a ponta → replay da DLQ + hardening do worker | alta | ✅ concluído (2026-09-30) | E4; maior bloco (XL) — pivô síncrono, uma compensação (estorno) | 0022 (✅ PR #51) · 0023 (✅ PR #52) · 0024 (✅ PR #53) · 0025 (✅ PR #54) · 0026 (✅ PR #55) · 0027 (✅ PR #56) |
| 8 | **E7 — Notificação: e-mail + QR** (**3 tasks** — refinamento E7): núcleo (portas pedido/sessão/catálogo, token HMAC, QR inline por CID, template, consumidor real com fake) → provedor Resend por HTTP cru + config + métricas/alertas → e-mail de estorno (`pedido.estornado`) | alta | ✅ concluído (2026-09-30) | E6; **sem domínio verificado o Resend só entrega ao dono da conta** (decisão do usuário: demo assim até haver domínio) | 0028 (✅ PR #58) · 0029 (✅ PR #59) · 0030 (✅ PR #60) |
| 9 | **E8 — SPA checkout + convidado/conta** (**5 tasks** — refinamento E8): backend do checkout/conta (Bearer opcional, retomada, meus pedidos, rota de teste só com fake) → SPA de sessão/login/meus pedidos (single-flight do refresh) → SPA de checkout com Payment Element → backend de consulta de convidado e ingresso `/i/*` → SPA de consulta/ingresso + **E2E do M4** | alta | ✅ concluído (2026-09-30) | E6–E7; fechou o **M4** | 0031 (✅ PR #62) · 0032 (✅ PR #63) · 0033 (✅ PR #64) · 0034 (✅ PR #65) · 0035 (✅ PR #66) |
| 10 | **E9 — Backoffice restante** (**4 tasks** — refinamento E9, ADR 0011; T3 dividida por tamanho): cancelamento do cliente (janela 2h) e da sessão com vendidos + botão na SPA → trilha de auditoria + consulta e cancelamento de pedido pelo operador → **telas do operador** (filmes, salas, sessões, pedidos) | média | ✅ concluído (2026-10-01) | E6; manter mínimo — backoffice é sumidouro de horas | 0036 (✅ PR #68) · 0037 (✅ PR #69) · 0038 (✅ PR #70) · 0039 (✅ PR #71) |
| 11 | **E10 — Observabilidade completa** (**2 tasks** — refinamento E10, ADR 0012): Tempo + exporter OTLP + **tail sampling no Alloy** (erros, > 300 ms, 10%) + correlação logs↔traces → dashboards de negócio (funil, compensações/cancelamentos, ocupação, e-mail/gateway) + alertas (purga, textos de estorno) + **watchdog** por heartbeat (healthchecks.io; monitor HTTP no E0c-CD) | média | em andamento | E6 (a saga é o que dá valor ao tracing) | 0040 · 0041 |
| 12 | **E11 — Hardening + backup** (1–2 tasks): NSG/portas, senhas de infra, Grafana com senha, pg_dump noturno cifrado → Object Storage + restore validado, `disk > 80%` alerta nº 1 | alta | pendente | E0 (parcial no skeleton); doc.md §14.5 bloqueia deploy público de produção | — |
| 13 | **E12 — Teste de carga k6** (1–2 tasks): modo load-test (fakes de gateway/e-mail), baseline → ramp → SLO @ 300 req/s → soak → cenário de disputa; gerador fora da VM; p95 server-side; **M5** | alta | pendente | E6, E10; nightly/manual, nunca gate de PR | — |
| 14 | **Pós-MVP (fase 2)**: check-in QR na entrada (validação single-use pelo operador), tradução EN, relatórios do operador, lembrete de sessão, PIX/Mercado Pago, mobile, 2FA operador, CAPTCHA condicional | baixa | backlog | MVP entregue | — |

## Riscos do roadmap (top 5, do parecer de viabilidade)

1. **"Escrever Java em Go"** — wiring manual no `main`, interfaces só com segundo implementador real, Context7 antes de qualquer API de lib.
2. **Saga superdimensionada no imaginário** — com e-mail pós-pivô, a saga real é hold→cobrança→emissão com **uma** compensação verdadeira; não deixar a palavra inflar PRDs.
3. **Mapa de assentos no React** — grade simples do template JSON, nunca editor visual; polling, não WebSocket.
4. **Backoffice como sumidouro de horas** — escopo mínimo vigiado a cada task (nenhum parecer o dimensionou grande, e é onde projetos solo afundam).
5. **Capacidade Oracle A1** — VM criada na semana 1; "VM descartável, tudo reconstituível por código".

## Como usar

1. Execute o item 0 (pré-bootstrap — sem código, sem PRD; só ambiente e contas).
2. `criar-task` sobre a primeira fatia do E0 → refinamento (5 agentes) → PRD → implementação → auditoria → push → PR.
3. Gate de cobertura 80%/pacote de domínio ativa a partir do E2 (decisão backend-dev); k6 nightly/manual a partir do E12.
4. Candidatas a ADR do `doc.md` §11: propor a criação (com autorização explícita) no refinamento da task que materializa cada decisão.
