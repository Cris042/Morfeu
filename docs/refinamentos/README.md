# Refinamentos por épico

Registro das cerimônias de refinamento multiagente (roles.md §6.14): **uma por épico**, antes da primeira task. Cada arquivo contém pareceres dos 5 agentes, debate, conclusão e **exigências por task** — que os PRDs do épico DEVEM incorporar.

| Épico | Arquivo | Data | Status |
|---|---|---|---|
| E0 — Walking skeleton | [`E0-walking-skeleton.md`](E0-walking-skeleton.md) | 2026-07-09 | concluído — 2 perguntas escaladas **respondidas em 2026-07-11**: reordenação aprovada (roadmap atualizado) e ADR 0007 de mensageria autorizado/criado |
| E1 — Identidade | [`E1-identidade.md`](E1-identidade.md) | 2026-09-29 | concluído — 4 perguntas escaladas **respondidas no mesmo dia** (rate limit c/ fallback em memória; TTL 10 min / 7 d; SameSite Strict; sem janela de graça + single-flight no SPA) |
| E2 — Catálogo + TMDB | [`E2-catalogo-tmdb.md`](E2-catalogo-tmdb.md) | 2026-09-29 | concluído — 1 pergunta escalada **respondida no mesmo dia** (pôster por hotlink da CDN do TMDB); demais divergências por consenso |
| E3 — Sessões e salas | [`E3-sessoes-salas.md`](E3-sessoes-salas.md) | 2026-09-29 | concluído — 2 perguntas escaladas **respondidas no mesmo dia** (limpeza de 20 min; telas do operador → E5/E9, só API no E3) |
| E4 — Reserva (backend) | [`E4-reserva.md`](E4-reserva.md) | 2026-09-29 | concluído — 3 perguntas escaladas **respondidas no mesmo dia** (ADR 0008 autorizado; token de carrinho em cookie HttpOnly + anti-CSRF; 6 holds/dono, 30/min por IP e 20/min por dono) |

> Nota de transição: a task 0001 (E0a) foi refinada e entregue no modelo antigo (por task). O refinamento por épico do E0 (acima) **substitui** o refinamento antigo da task 0002 (E0b): reescopou o rascunho (corte de `FilmUpdated`, fronteiras plataforma×domínio, publisher confirms) — o PRD da 0002 DEVE consumir as exigências do refinamento do épico.
