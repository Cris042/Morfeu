# Task 0016 — Reserva: ocupação pública, cache e alertas (E4, T2)

- **Data:** 2026-09-29
- **Status:** em andamento
- **Branch:** `feature/0016-reserva-ocupacao` (da main `36613c3`, pós-0015)
- **PRD:** docs/prd/0016-reserva-ocupacao.md
- **Item do roadmap:** E4 — Reserva (task 2/2; fecha o épico). Refinamento: `docs/refinamentos/E4-reserva.md` §T2.

## Objetivo

Expor ao SPA do E5 quais assentos de uma sessão estão ocupados (polling de 3–5 s), com cache Redis curto que absorve o polling, e alertar quando a trava se comporta de forma anormal.

## Escopo

`GET /sessoes/{id}/ocupacao` (só códigos ocupados), cache `reserva:ocupacao:{id}` com TTL de 3 s e sem invalidação ativa, `Cache-Control` curto, 2 alertas no Grafana (recusas anormais; sweeper parado), fechamento de status da 0015.

## Fora de escopo

UI do mapa (E5); conversão em ingresso (E6); purge de holds (hardening).

## Arquivos esperados

~16 (lista no PRD).

## Dependências esperadas

Nenhuma nova.

## Critérios de aceite

- [ ] Ocupação só com códigos de holds vivos, sem dono, id ou prazo.
- [ ] Sessão cancelada, iniciada ou inexistente → 404.
- [ ] Cache: TTL ≤ 3 s no Redis; hold novo não aparece enquanto o cache vale, e aparece depois que a chave expira.
- [ ] Hold vencido não conta como ocupado.
- [ ] Stack de observabilidade sobe com 9 regras de alerta.
- [ ] CI verde.

## Riscos

- Polling martelando o banco → cache hit dispensa até a consulta da porta.

## Estimativa de impacto

Baixo em código, nenhum em banco (consulta sobre o índice único parcial), baixo em infra (2 alertas).
