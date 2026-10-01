# Refinamentos por épico

Registro das cerimônias de refinamento multiagente (roles.md §6.14): **uma por épico**, antes da primeira task. Cada arquivo contém pareceres dos 5 agentes, debate, conclusão e **exigências por task** — que os PRDs do épico DEVEM incorporar.

| Épico | Arquivo | Data | Status |
|---|---|---|---|
| E0 — Walking skeleton | [`E0-walking-skeleton.md`](E0-walking-skeleton.md) | 2026-07-09 | concluído — 2 perguntas escaladas **respondidas em 2026-07-11**: reordenação aprovada (roadmap atualizado) e ADR 0007 de mensageria autorizado/criado |
| E1 — Identidade | [`E1-identidade.md`](E1-identidade.md) | 2026-09-29 | concluído — 4 perguntas escaladas **respondidas no mesmo dia** (rate limit c/ fallback em memória; TTL 10 min / 7 d; SameSite Strict; sem janela de graça + single-flight no SPA) |
| E2 — Catálogo + TMDB | [`E2-catalogo-tmdb.md`](E2-catalogo-tmdb.md) | 2026-09-29 | concluído — 1 pergunta escalada **respondida no mesmo dia** (pôster por hotlink da CDN do TMDB); demais divergências por consenso |
| E3 — Sessões e salas | [`E3-sessoes-salas.md`](E3-sessoes-salas.md) | 2026-09-29 | concluído — 2 perguntas escaladas **respondidas no mesmo dia** (limpeza de 20 min; telas do operador → E5/E9, só API no E3) |
| E4 — Reserva (backend) | [`E4-reserva.md`](E4-reserva.md) | 2026-09-29 | concluído — 3 perguntas escaladas **respondidas no mesmo dia** (ADR 0008 autorizado; token de carrinho em cookie HttpOnly + anti-CSRF; 6 holds/dono, 30/min por IP e 20/min por dono) |
| E5 — SPA cliente, parte 1 | [`E5-spa-cliente.md`](E5-spa-cliente.md) | 2026-09-29 | concluído — 4 perguntas escaladas **respondidas no mesmo dia** (ADR 0009 autorizado; telas do operador → E9; E2E do M3 no CI; headers de segurança definidos na T1 e aplicados no E0c-CD) |
| E6 — Saga do checkout | [`E6-saga-checkout.md`](E6-saga-checkout.md) | 2026-09-30 | concluído — 4 perguntas escaladas **respondidas no mesmo dia** (ADR 0010 autorizado; PaymentIntent + Payment Element; token do ingresso por HMAC; 6 tasks) |
| E7 — Notificação: e-mail + QR | [`E7-notificacao.md`](E7-notificacao.md) | 2026-09-30 | concluído — 3 perguntas escaladas **respondidas no mesmo dia** (Resend com demo só para o dono até haver domínio; estorno no E7 → 3 tasks; QR com skip2/go-qrcode) |
| E8 — SPA checkout + convidado/conta | [`E8-spa-checkout.md`](E8-spa-checkout.md) | 2026-09-30 | concluído — 4 perguntas escaladas **respondidas no mesmo dia** (rota `__teste/pagar` só com gateway fake; "Meus pedidos" no E8; página `/i/*` pela SPA; axe no E2E); 5 tasks; sem ADR |
| E9 — Backoffice restante | [`E9-backoffice.md`](E9-backoffice.md) | 2026-10-01 | concluído — 3 perguntas escaladas **respondidas no mesmo dia** (cancelamento individual pelo operador incluído; sessão cancelada por porta + TX única; ADR 0011 autorizado); 3 tasks |
| E10 — Observabilidade completa | [`E10-observabilidade.md`](E10-observabilidade.md) | 2026-10-01 | concluído — 2 perguntas escaladas **respondidas no mesmo dia** (ADR 0012 — tail sampling no Alloy; watchdog por healthchecks.io agora, monitor HTTP no deploy); 2 tasks |
| E11 — Hardening + backup | [`E11-hardening-backup.md`](E11-hardening-backup.md) | 2026-10-01 | concluído — 3 perguntas escaladas **respondidas no mesmo dia** (ADR 0013 autorizado; chave age no gerenciador + cópia offline; limpeza operacional como 0044, sem pedidos); 3 tasks |

> Nota de transição: a task 0001 (E0a) foi refinada e entregue no modelo antigo (por task). O refinamento por épico do E0 (acima) **substitui** o refinamento antigo da task 0002 (E0b): reescopou o rascunho (corte de `FilmUpdated`, fronteiras plataforma×domínio, publisher confirms) — o PRD da 0002 DEVE consumir as exigências do refinamento do épico.
