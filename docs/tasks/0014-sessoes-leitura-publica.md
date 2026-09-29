# Task 0014 — Sessões: leitura pública, mapa de assentos e cache (E3, T2)

- **Data:** 2026-09-29
- **Status:** concluída
- **Branch:** `feature/0014-sessoes-leitura-publica` (da main `c5478a2`, pós-0013)
- **PRD:** docs/prd/0014-sessoes-leitura-publica.md
- **Item do roadmap:** E3 — Sessões e salas (task 2/2; fecha o épico). Refinamento: `docs/refinamentos/E3-sessoes-salas.md` §T2.

## Objetivo

Expor ao público (e ao futuro SPA do E5) as sessões futuras de um filme e o mapa de assentos de uma sessão, com cache Redis invalidado nas escritas; dar filtros à listagem do operador; aplicar o ajuste da auditoria 0013 (comparação de layout independente da ordem).

## Escopo

`GET /filmes/{id}/sessoes` (público, cache TTL 60 s + invalidação em criar/cancelar), `GET /sessoes/{id}/mapa` (público, layout + códigos de assento), `GET /backoffice/sessoes?sala_id=&data=`, comparação semântica de layout, rotas públicas também em `-mode=worker`.

## Fora de escopo

Ocupação/holds no mapa (E4); telas (E5/E9); fuso de exibição (borda do SPA).

## Arquivos esperados

~17 (lista no PRD), incluindo fechamento de status das tasks 0012 e 0013.

## Critérios de aceite

- [ ] Público: só sessões `agendada` futuras do filme, ordenadas, sem campos de filme; 404-livre (lista vazia) para filme sem sessões.
- [ ] Mapa: layout + assentos (códigos determinísticos, PCD) para sessão agendada futura; 404 para cancelada/passada/inexistente.
- [ ] Cache: populado na leitura, apagado ao criar/cancelar sessão do filme (Redis real); sessão que começou durante o TTL não aparece.
- [ ] Filtros do operador por sala e dia.
- [ ] Layout reenviado em outra ordem não conta como mudança.
- [ ] CI verde.

## Riscos

- Cache servir sessão já iniciada → refiltragem por horário na leitura.

## Estimativa de impacto

Médio em código, nenhum em banco (queries novas sobre índices existentes), nenhum em infra.
